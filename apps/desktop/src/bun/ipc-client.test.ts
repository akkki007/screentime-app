import { afterEach, beforeEach, describe, expect, test } from 'bun:test';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { IPC_VERSION } from '@screentime/shared';
import { DaemonConnection } from './daemon-connection';
import { DaemonClient } from './ipc-client';

let dir: string;
let sockPath: string;
let server: ReturnType<typeof Bun.listen> | undefined;
const sockets = new Set<{ write: (s: string) => unknown; end: () => unknown }>();

/** A stand-in daemon: answers version and `echo`, ignores `hang`, and can push notifications. */
function startFakeDaemon({ rejectVersion = false } = {}) {
  server = Bun.listen<{ buffer: string }>({
    unix: sockPath,
    socket: {
      open(socket) {
        socket.data = { buffer: '' };
        sockets.add(socket);
      },
      close(socket) {
        sockets.delete(socket);
      },
      data(socket, chunk) {
        socket.data.buffer += chunk.toString('utf8');
        let i: number;
        // biome-ignore lint/suspicious/noAssignInExpressions: line splitter
        while ((i = socket.data.buffer.indexOf('\n')) !== -1) {
          const req = JSON.parse(socket.data.buffer.slice(0, i));
          socket.data.buffer = socket.data.buffer.slice(i + 1);
          if (req.method === 'version') {
            const ok = !rejectVersion && req.params.major === IPC_VERSION;
            socket.write(
              `${JSON.stringify(ok ? { id: req.id, result: { major: IPC_VERSION } } : { id: req.id, error: { message: 'unsupported major version' } })}\n`,
            );
          } else if (req.method === 'echo') {
            socket.write(`${JSON.stringify({ id: req.id, result: req.params })}\n`);
          } else if (req.method === 'fail') {
            socket.write(`${JSON.stringify({ id: req.id, error: { message: 'nope' } })}\n`);
          }
          // 'hang' gets no reply
        }
      },
    },
  });
}

function stopFakeDaemon() {
  for (const s of sockets) s.end();
  sockets.clear();
  server?.stop(true);
  server = undefined;
}

const sleep = (ms: number) => new Promise((r) => setTimeout(r, ms));
async function until(cond: () => boolean, ms = 3_000) {
  const deadline = Date.now() + ms;
  while (!cond()) {
    if (Date.now() > deadline) throw new Error('timed out');
    await sleep(10);
  }
}

beforeEach(() => {
  dir = mkdtempSync(join(tmpdir(), 'screentime-ui-'));
  sockPath = join(dir, 'daemon.sock');
});
afterEach(() => {
  stopFakeDaemon();
  rmSync(dir, { recursive: true, force: true });
});

describe('DaemonClient', () => {
  test('round-trips calls and surfaces daemon errors', async () => {
    startFakeDaemon();
    const client = new DaemonClient();
    await client.connect(sockPath);
    expect(await client.call<{ a: number }>('echo', { a: 1 })).toEqual({ a: 1 });
    await expect(client.call('fail')).rejects.toThrow('nope');
    client.close();
  });

  test('delivers server notifications', async () => {
    startFakeDaemon();
    const client = new DaemonClient();
    const received: [string, unknown][] = [];
    client.onNotification = (m, p) => received.push([m, p]);
    await client.connect(sockPath);

    for (const s of sockets)
      s.write(
        `${JSON.stringify({ jsonrpc: '2.0', method: 'event.focus', params: { appId: 'a', since: 1 } })}\n`,
      );
    await until(() => received.length === 1);
    expect(received[0]).toEqual(['event.focus', { appId: 'a', since: 1 }]);
    client.close();
  });

  test('rejects connect, and stays disconnected, when the daemon refuses the version', async () => {
    startFakeDaemon({ rejectVersion: true });
    const client = new DaemonClient();
    await expect(client.connect(sockPath)).rejects.toThrow('unsupported major version');
    expect(client.connected).toBe(false);
  });

  test('fails in-flight calls and reports a close when the daemon drops', async () => {
    startFakeDaemon();
    const client = new DaemonClient();
    let closed = 0;
    client.onClose = () => closed++;
    await client.connect(sockPath);

    const inflight = client.call('hang');
    stopFakeDaemon();
    await expect(inflight).rejects.toThrow('closed');
    await until(() => closed === 1);
    expect(client.connected).toBe(false);
    await expect(client.call('echo')).rejects.toThrow('not connected');
  });
});

describe('DaemonConnection', () => {
  test('reports a missing daemon once, then connects when it appears', async () => {
    const states: { connected: boolean; error?: string }[] = [];
    const conn = new DaemonConnection({
      socketPath: sockPath,
      retryMs: 20,
      onNotification: () => {},
      onState: (s) => states.push(s),
    });
    conn.start();

    await sleep(120); // several failed attempts
    expect(states.filter((s) => !s.connected)).toHaveLength(1); // not one per retry
    await expect(conn.call('echo')).rejects.toThrow('not running');

    startFakeDaemon();
    await until(() => conn.connected);
    expect(states.at(-1)).toEqual({ connected: true });
    expect(await conn.call<number>('echo', 7)).toBe(7);
    conn.stop();
  });

  test('reconnects after the daemon restarts', async () => {
    startFakeDaemon();
    const states: boolean[] = [];
    const conn = new DaemonConnection({
      socketPath: sockPath,
      retryMs: 20,
      onNotification: () => {},
      onState: (s) => states.push(s.connected),
    });
    conn.start();
    await until(() => conn.connected);

    stopFakeDaemon();
    await until(() => !conn.connected);
    startFakeDaemon();
    await until(() => conn.connected);

    expect(states).toEqual([true, false, true]);
    conn.stop();
  });

  test('stop() prevents any further reconnects', async () => {
    const conn = new DaemonConnection({
      socketPath: sockPath,
      retryMs: 10,
      onNotification: () => {},
      onState: () => {},
    });
    conn.start();
    conn.stop();
    startFakeDaemon();
    await sleep(80);
    expect(conn.connected).toBe(false);
  });
});
