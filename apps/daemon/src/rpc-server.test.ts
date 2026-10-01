import { afterEach, beforeEach, describe, expect, test } from 'bun:test';
import { mkdtempSync, rmSync, statSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { openDb } from '@screentime/db';
import { IPC_VERSION } from '@screentime/shared';
import { type RpcServer, isSocketLive, startRpcServer } from './rpc-server';

let dir: string;
let sockPath: string;
let db: ReturnType<typeof openDb>;
let server: RpcServer;
const received: unknown[] = [];

async function connect() {
  const lines: string[] = [];
  let buffer = '';
  let wake: (() => void) | undefined;

  const socket = await Bun.connect({
    unix: sockPath,
    socket: {
      data(_s, chunk) {
        buffer += chunk.toString('utf8');
        let i: number;
        // biome-ignore lint/suspicious/noAssignInExpressions: line splitter
        while ((i = buffer.indexOf('\n')) !== -1) {
          lines.push(buffer.slice(0, i));
          buffer = buffer.slice(i + 1);
        }
        wake?.();
      },
    },
  });

  const nextLine = async (): Promise<Record<string, unknown>> => {
    const deadline = Date.now() + 2_000;
    while (lines.length === 0) {
      if (Date.now() > deadline) throw new Error('timed out waiting for a message');
      await new Promise<void>((resolve) => {
        wake = resolve;
        setTimeout(resolve, 50);
      });
    }
    return JSON.parse(lines.shift() as string);
  };

  return { socket, send: (m: unknown) => socket.write(`${JSON.stringify(m)}\n`), nextLine };
}

beforeEach(() => {
  dir = mkdtempSync(join(tmpdir(), 'screentime-rpc-'));
  sockPath = join(dir, 'rt', 'daemon.sock');
  db = openDb(join(dir, 't.db'));
  received.length = 0;
  server = startRpcServer(
    db,
    {
      'tracker.resume': () => ({ ok: true }),
      'tracker.pause': ({ minutes }: { minutes: number }) => ({ resumeAt: minutes }),
      'limits.list': () => {
        throw new Error('boom');
      },
      'browser.activeTab': (p) => {
        received.push(p);
      },
    },
    sockPath,
  );
});

afterEach(() => {
  server.stop();
  db.close();
  rmSync(dir, { recursive: true, force: true });
});

describe('rpc server', () => {
  test('creates the socket with mode 0600', () => {
    expect(statSync(sockPath).mode & 0o777).toBe(0o600);
  });

  test('isSocketLive detects a running server, and a stale socket file', async () => {
    expect(await isSocketLive(sockPath)).toBe(true);
    server.stop();
    expect(await isSocketLive(sockPath)).toBe(false);
    expect(await isSocketLive(join(dir, 'never-existed.sock'))).toBe(false);
  });

  test('handshake accepts the current major version and rejects others', async () => {
    const c = await connect();
    c.send({ jsonrpc: '2.0', id: 1, method: 'version', params: { major: IPC_VERSION } });
    expect((await c.nextLine()).result).toEqual({ major: IPC_VERSION });

    c.send({ jsonrpc: '2.0', id: 2, method: 'version', params: { major: 99 } });
    expect(((await c.nextLine()).error as { message: string }).message).toContain('unsupported');
    c.socket.end();
  });

  test('validates params, returns results, and reports handler errors', async () => {
    const c = await connect();

    c.send({ jsonrpc: '2.0', id: 1, method: 'tracker.pause', params: { minutes: 5 } });
    expect((await c.nextLine()).result).toEqual({ resumeAt: 5 });

    c.send({ jsonrpc: '2.0', id: 2, method: 'tracker.pause', params: { minutes: -1 } });
    expect(((await c.nextLine()).error as { message: string }).message).toMatch(/^minutes: /);

    c.send({ jsonrpc: '2.0', id: 3, method: 'limits.list' });
    expect(((await c.nextLine()).error as { message: string }).message).toBe('boom');

    c.send({ jsonrpc: '2.0', id: 4, method: 'no.such.method' });
    expect(((await c.nextLine()).error as { code: number }).code).toBe(-32601);
    c.socket.end();
  });

  test('survives garbage, null and malformed browser messages', async () => {
    const c = await connect();
    c.socket.write('not json\nnull\n42\n{"method":7}\n');
    c.send({ jsonrpc: '2.0', method: 'browser.activeTab', params: { domain: 5 } }); // wrong type
    c.send({
      jsonrpc: '2.0',
      method: 'browser.activeTab',
      params: { domain: 'a.com', active: true },
    });

    // Daemon still answers afterwards.
    c.send({ jsonrpc: '2.0', id: 1, method: 'tracker.resume' });
    expect((await c.nextLine()).result).toEqual({ ok: true });
    expect(received).toEqual([{ domain: 'a.com', active: true }]);
    c.socket.end();
  });

  test('broadcasts notifications to every connected client', async () => {
    const a = await connect();
    const b = await connect();
    // Make sure both sockets are registered before broadcasting.
    a.send({ jsonrpc: '2.0', id: 1, method: 'tracker.resume' });
    b.send({ jsonrpc: '2.0', id: 1, method: 'tracker.resume' });
    await a.nextLine();
    await b.nextLine();

    server.broadcast('event.focus', { appId: 'org.mozilla.firefox', since: 123 });
    for (const c of [a, b]) {
      expect(await c.nextLine()).toEqual({
        jsonrpc: '2.0',
        method: 'event.focus',
        params: { appId: 'org.mozilla.firefox', since: 123 },
      });
      c.socket.end();
    }
  });
});
