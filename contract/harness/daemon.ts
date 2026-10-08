import { Database } from 'bun:sqlite';
import { mkdirSync, mkdtempSync, readFileSync, readdirSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
/**
 * Starts a daemon in an isolated, deterministic environment and talks to it
 * over its socket. Used both to record fixtures and to replay them.
 *
 * The daemon command defaults to bin/screentimed (go build -o bin/screentimed
 * ./cmd/screentimed). Set CONTRACT_DAEMON_CMD (space-separated) to run another
 * build against the same fixtures.
 */
import { type Subprocess, spawn } from 'bun';

const ROOT = join(import.meta.dir, '..', '..');
export const FIXTURES_DIR = join(import.meta.dir, '..', 'fixtures');

/** What every fixture runs under. */
export type FixtureEnv = {
  /** Frozen clock, Unix ms. */
  now: number;
  /** IANA zone for every local-day calculation. */
  tz: string;
  /** SQL file in fixtures/ applied after migrations, before the daemon starts. */
  seed: string;
};

export function daemonCommand(): string[] {
  const custom = process.env.CONTRACT_DAEMON_CMD;
  if (custom) return custom.split(' ').filter(Boolean);
  return [join(ROOT, 'bin', 'screentimed')];
}

const MIGRATIONS_DIR = join(ROOT, 'internal', 'store', 'migrations');

/** A database at the current schema, made the way the daemon does: numbered files, PRAGMA user_version. */
function createDb(path: string): Database {
  mkdirSync(join(path, '..'), { recursive: true });
  const db = new Database(path, { create: true });
  const files = readdirSync(MIGRATIONS_DIR)
    .filter((f) => f.endsWith('.sql'))
    .sort();
  files.forEach((file, i) => {
    db.exec(readFileSync(join(MIGRATIONS_DIR, file), 'utf8'));
    db.exec(`PRAGMA user_version = ${i + 1}`);
  });
  return db;
}

export type Message = Record<string, unknown>;

export type Daemon = {
  /**
   * Sends one line (an object, or a raw string for malformed input) and
   * returns every message received until the response with the same id
   * arrives, plus anything that follows within `quietMs` (default 30 ms).
   * Steps without an id have nothing to wait for, so they wait 150 ms.
   */
  exchange: (send: Message | string, quietMs?: number) => Promise<Message[]>;
  stop: () => Promise<void>;
  /** Daemon stdout/stderr so far, for diagnosing failures. */
  log: () => string;
};

const sleep = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms));

export async function startDaemon(env: FixtureEnv): Promise<Daemon> {
  const dir = mkdtempSync(join(tmpdir(), 'st-contract-'));
  const dataHome = join(dir, 'data');
  const runtimeDir = join(dir, 'run');
  const home = join(dir, 'home');
  const emptyDataDirs = join(dir, 'share');
  for (const d of [dataHome, home, emptyDataDirs]) mkdirSync(d, { recursive: true });
  mkdirSync(runtimeDir, { recursive: true, mode: 0o700 });

  const db = createDb(join(dataHome, 'screentime', 'screentime.db'));
  db.exec(readFileSync(join(FIXTURES_DIR, env.seed), 'utf8'));
  db.close();

  const output: string[] = [];
  const child: Subprocess<'ignore', 'pipe', 'pipe'> = spawn(daemonCommand(), {
    cwd: ROOT,
    stdin: 'ignore',
    stdout: 'pipe',
    stderr: 'pipe',
    env: {
      PATH: process.env.PATH ?? '/usr/bin:/bin',
      HOME: home,
      XDG_DATA_HOME: dataHome,
      XDG_DATA_DIRS: emptyDataDirs,
      XDG_RUNTIME_DIR: runtimeDir,
      TZ: env.tz,
      SCREENTIME_FAKE_NOW: String(env.now),
      SCREENTIME_FOCUS_PROVIDER: 'none',
      // No session bus: nothing reaches the real desktop (no notifications).
      DBUS_SESSION_BUS_ADDRESS: 'unix:path=/nonexistent',
    },
  });
  for (const stream of [child.stdout, child.stderr]) {
    (async () => {
      for await (const chunk of stream) output.push(new TextDecoder().decode(chunk));
    })();
  }

  const lines: Message[] = [];
  let buffer = '';
  const sockPath = join(runtimeDir, 'screentime', 'daemon.sock');
  let socket: Awaited<ReturnType<typeof Bun.connect>> | undefined;
  for (let attempt = 0; attempt < 100 && !socket; attempt++) {
    if (child.exitCode !== null) break;
    try {
      socket = await Bun.connect({
        unix: sockPath,
        socket: {
          data(_s, chunk) {
            buffer += chunk.toString('utf8');
            let i = buffer.indexOf('\n');
            while (i !== -1) {
              lines.push(JSON.parse(buffer.slice(0, i)));
              buffer = buffer.slice(i + 1);
              i = buffer.indexOf('\n');
            }
          },
        },
      });
    } catch {
      await sleep(50);
    }
  }
  if (!socket) {
    child.kill();
    throw new Error(`daemon did not start:\n${output.join('')}`);
  }
  const connected = socket;

  return {
    async exchange(send, quietMs) {
      const start = lines.length;
      connected.write(`${typeof send === 'string' ? send : JSON.stringify(send)}\n`);
      const id = typeof send === 'string' ? undefined : send.id;
      const hasId = id !== undefined && id !== null;
      if (hasId) {
        const deadline = Date.now() + 3_000;
        while (!lines.slice(start).some((m) => m.id === id)) {
          if (Date.now() > deadline) throw new Error(`no response to id ${id}`);
          await sleep(5);
        }
      }
      await sleep(quietMs ?? (hasId ? 30 : 150));
      return lines.slice(start);
    },
    async stop() {
      connected.end();
      child.kill('SIGTERM');
      await child.exited;
      rmSync(dir, { recursive: true, force: true });
    },
    log: () => output.join(''),
  };
}
