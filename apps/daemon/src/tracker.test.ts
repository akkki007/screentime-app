import { afterEach, beforeEach, describe, expect, test } from 'bun:test';
import { existsSync, rmSync } from 'node:fs';
import { openDb } from '@screentime/db';
import type { FocusProvider, FocusedWindow } from '@screentime/shared';
import { Tracker, type TrackerOptions, normalizeAppId } from './tracker';

const TEST_DB_PATH = `${import.meta.dir}/.test-tracker.db`;
const THRESHOLD = 180_000;

type Row = { app_id: string; title: string | null; start_ts: number; end_ts: number };

/** Harness: a fake focus provider plus a manually advanced clock. */
function setup(options: TrackerOptions = {}) {
  const db = openDb(TEST_DB_PATH);
  let clock = 1_000_000;
  let emitFocus: (w: FocusedWindow) => void = () => {};
  let emitIdle: (idle: boolean) => void = () => {};

  const provider: FocusProvider = {
    id: 'fake',
    isAvailable: async () => true,
    onFocusChange: (cb) => {
      emitFocus = cb;
      return () => {};
    },
    onIdleChange: (cb) => {
      emitIdle = cb;
      return () => {};
    },
  };

  const tracker = new Tracker(db, provider, {
    idleThresholdMs: THRESHOLD,
    now: () => clock,
    ...options,
  });
  tracker.start();

  return {
    db,
    tracker,
    now: () => clock,
    advance: (ms: number) => {
      clock += ms;
    },
    focus: (appId: string, title?: string) => emitFocus({ appId, title, ts: clock }),
    idle: (idle: boolean) => emitIdle(idle),
    /** Runs the heartbeat the way the interval would. */
    beat: () => (tracker as unknown as { handleHeartbeat(): void }).handleHeartbeat(),
    rows: () =>
      db
        .query(
          'SELECT apps.app_id, title, start_ts, end_ts FROM sessions JOIN apps ON apps.id = sessions.app_id ORDER BY sessions.id',
        )
        .all() as Row[],
  };
}

let t: ReturnType<typeof setup>;
beforeEach(() => {
  t = setup();
});
afterEach(() => {
  t.tracker.stop();
  t.db.close();
  for (const suffix of ['', '-wal', '-shm']) {
    const path = `${TEST_DB_PATH}${suffix}`;
    if (existsSync(path)) rmSync(path);
  }
});

describe('window titles', () => {
  test('are not stored by default', () => {
    t.focus('org.mozilla.firefox', 'secret bank page');
    t.advance(1_000);
    t.focus('org.mozilla.firefox', 'another secret');
    expect(t.rows().map((r) => r.title)).toEqual([null]);
  });

  test('are stored when the user opts in', () => {
    t.tracker.stop();
    t.db.close();
    rmSync(TEST_DB_PATH);
    t = setup({ captureTitles: true });
    t.focus('org.mozilla.firefox', 'docs');
    expect(t.rows().map((r) => r.title)).toEqual(['docs']);
  });
});

describe('app identity', () => {
  test('strips the .desktop suffix', () => {
    expect(normalizeAppId('org.mozilla.firefox.desktop')).toBe('org.mozilla.firefox');
    expect(normalizeAppId('Xterm')).toBe('Xterm');
    t.focus('org.gnome.Ptyxis.desktop');
    expect(t.rows()[0]?.app_id).toBe('org.gnome.Ptyxis');
  });
});

describe('merge rule', () => {
  test('same app within the gap extends one session', () => {
    t.focus('a');
    t.advance(5_000);
    t.focus('a');
    expect(t.rows()).toHaveLength(1);
    expect(t.rows()[0]?.end_ts).toBe(t.now());
  });

  test('switching apps closes the old session and opens a new one', () => {
    const start = t.now();
    t.focus('a');
    t.advance(4_000);
    t.beat();
    t.advance(1_000);
    t.focus('b');
    const [a, b] = t.rows();
    expect(a).toMatchObject({ app_id: 'a', start_ts: start, end_ts: start + 4_000 });
    expect(b).toMatchObject({ app_id: 'b', start_ts: t.now() });
  });
});

describe('idle', () => {
  test('ends the session at the start of the idle period', () => {
    const start = t.now();
    t.focus('a');
    // 1 minute of real use, then 3 minutes idle while heartbeats keep ticking.
    for (let i = 1; i <= 48; i++) {
      t.advance(5_000);
      t.beat();
    }
    t.idle(true); // fires 4 minutes in; idle began 3 minutes ago = 1 minute in
    expect(t.rows()[0]?.end_ts).toBe(start + 4 * 60_000 - THRESHOLD);
  });

  test('resumes tracking the same window when activity returns', () => {
    t.focus('a');
    t.advance(THRESHOLD);
    t.idle(true);
    t.advance(60_000);
    t.idle(false); // no focus event: the user just touched the mouse
    const rows = t.rows();
    expect(rows).toHaveLength(2);
    expect(rows[1]).toMatchObject({ app_id: 'a', start_ts: t.now() });
  });

  test('focus changes while idle are remembered but not tracked', () => {
    t.focus('a');
    t.advance(THRESHOLD);
    t.idle(true);
    t.focus('b');
    expect(t.rows()).toHaveLength(1);
    t.idle(false);
    expect(t.rows()[1]?.app_id).toBe('b');
  });
});

describe('suspend', () => {
  test('a long heartbeat gap splits the session instead of stretching it', () => {
    const start = t.now();
    t.focus('a');
    t.advance(5_000);
    t.beat();
    t.advance(8 * 3_600_000); // laptop asleep for 8 hours
    t.beat();
    const [before, after] = t.rows();
    expect(before).toMatchObject({ start_ts: start, end_ts: start + 5_000 });
    expect(after?.start_ts).toBe(t.now());
  });
});
