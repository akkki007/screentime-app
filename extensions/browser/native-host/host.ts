/**
 * Native messaging host: a small Bun script the browser launches as a
 * subprocess. Reads length-prefixed JSON messages from stdin (the Chrome
 * native messaging framing: a 4-byte little-endian length, then the UTF-8
 * JSON payload) and forwards `{domain, active}` to the daemon's Unix socket
 * as a `browser.activeTab` notification.
 *
 * Installed per-browser via a native messaging host manifest pointing at
 * this script's compiled output — see extensions/browser/README.md.
 */
import { runtimeSocketPath } from '@screentime/shared';

async function main() {
  const socket = await Bun.connect({
    unix: runtimeSocketPath(),
    socket: {
      data() {
        // The daemon doesn't reply to this notification; nothing to read.
      },
      error(_socket, err) {
        console.error('[native-host] daemon socket error:', err);
      },
    },
  });

  for await (const message of readMessages(process.stdin)) {
    socket.write(
      `${JSON.stringify({ jsonrpc: '2.0', method: 'browser.activeTab', params: message })}\n`,
    );
  }
}

async function* readMessages(stdin: typeof process.stdin): AsyncGenerator<unknown> {
  let buffer = Buffer.alloc(0);

  for await (const chunk of stdin) {
    buffer = Buffer.concat([buffer, chunk as Buffer]);

    while (buffer.length >= 4) {
      const length = buffer.readUInt32LE(0);
      if (buffer.length < 4 + length) break;

      const payload = buffer.subarray(4, 4 + length).toString('utf8');
      buffer = buffer.subarray(4 + length);
      yield JSON.parse(payload);
    }
  }
}

main().catch((err) => {
  console.error('[native-host] fatal error:', err);
  process.exit(1);
});
