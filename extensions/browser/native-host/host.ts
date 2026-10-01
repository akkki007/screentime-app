/**
 * Native messaging host: a small Bun script the browser launches as a
 * subprocess. Reads length-prefixed JSON messages from stdin (see framing.ts)
 * and forwards `{domain, active}` to the daemon's Unix socket as a
 * `browser.activeTab` notification.
 *
 * Installed per-browser by scripts/install-native-host.ts. Note that stdout is
 * the browser's protocol channel, so everything diagnostic goes to stderr.
 */
import { runtimeSocketPath } from '@screentime/shared';
import { FrameDecoder } from './framing';

async function main() {
  const socket = await Bun.connect({
    unix: runtimeSocketPath(),
    socket: {
      data() {
        // The daemon doesn't reply to this notification; nothing to read.
      },
      close() {
        // The daemon went away. Exit so the browser respawns us (and so we
        // reconnect) on the next report instead of writing into a dead socket.
        process.exit(0);
      },
      error(_socket, err) {
        console.error('[native-host] daemon socket error:', err);
      },
    },
  });

  const decoder = new FrameDecoder();
  for await (const chunk of process.stdin) {
    for (const message of decoder.push(chunk as Buffer)) {
      socket.write(
        `${JSON.stringify({ jsonrpc: '2.0', method: 'browser.activeTab', params: message })}\n`,
      );
    }
  }
  // stdin closed: the browser is shutting us down.
  socket.end();
}

main().catch((err) => {
  console.error('[native-host] fatal error:', err);
  process.exit(1);
});
