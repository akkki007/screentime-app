import { afterEach, beforeEach, describe, expect, test } from 'bun:test';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { openDb } from '@screentime/db';
import {
  DEFAULT_SETTINGS,
  type LimitHitEvent,
  type ReminderEvent,
  type Settings,
} from '@screentime/shared';
import { ensureApp } from './apps';
import type { Notifier } from './notifier';
import { setLimit } from './queries';
import { RulesEngine, formatDuration } from './rules';
import type { TrackerState } from './tracker';

const MIN = 60_000;
const HOUR = 60 * MIN;
const start = new Date(2026, 0, 5, 10, 0).getTime(); // Mon 10:00 local

let dir: string;
let db: ReturnType<typeof openDb>;
let clock: number;
let settings: Settings;
let state: TrackerState;
let domain: string | undefined;
let notes: { title: string; body: string }[];
let hits: LimitHitEvent[];
let reminders: ReminderEvent[];
let engine: RulesEngine;

const notifier: Notifier = {
  notify: async (title, body) => {
    notes.push({ title, body });
  },
};

function addSession(appId: string, from: number, to: number) {
  const id = ensureApp(db, appId, () => undefined);
  db.query(
    "INSERT INTO sessions (app_id, start_ts, end_ts, source) VALUES (?, ?, ?, 'desktop')",
  ).run(id, from, to);
}

/** Advance the clock in 5 s heartbeats, ticking the engine each time. */
function run(ms: number) {
  for (let t = 0; t < ms; t += 5_000) {
    clock += 5_000;
    engine.tick();
  }
}

beforeEach(() => {
  dir = mkdtempSync(join(tmpdir(), 'screentime-rules-'));
  db = openDb(join(dir, 't.db'));
  clock = start;
  settings = { ...DEFAULT_SETTINGS };
  state = { paused: false, resumeAt: null, idle: false, currentAppId: null, since: null };
  domain = undefined;
  notes = [];
  hits = [];
  reminders = [];
  engine = new RulesEngine({
    db,
    notifier,
    now: () => clock,
    settings: () => settings,
    state: () => state,
    activeDomain: () => domain,
    emitLimitHit: (e) => hits.push(e),
    emitReminder: (e) => reminders.push(e),
  });
});
afterEach(() => {
  db.close();
  rmSync(dir, { recursive: true, force: true });
});

describe('daily limits', () => {
  test('fires once when usage crosses the limit, not before and not again', () => {
    const limit = setLimit(db, {
      targetType: 'app',
      target: 'org.mozilla.firefox',
      dailyMs: HOUR,
      action: 'notify',
    });
    addSession('org.mozilla.firefox', start - 59 * MIN, start);
    engine.tick();
    expect(hits).toHaveLength(0);

    addSession('org.mozilla.firefox', start, start + 2 * MIN);
    clock += 2 * MIN;
    engine.tick();
    expect(hits).toEqual([{ limitId: limit.id as number, action: 'notify' }]);
    expect(notes).toHaveLength(1);
    expect(notes[0]?.body).toContain('1 h');

    run(30 * MIN);
    expect(hits).toHaveLength(1);
  });

  test('counts only the part of a session inside today', () => {
    setLimit(db, { targetType: 'app', target: 'a', dailyMs: HOUR, action: 'notify' });
    // A session that began yesterday evening: only its 30 minutes after midnight count today.
    const midnight = new Date(2026, 0, 5).getTime();
    addSession('a', midnight - 5 * HOUR, midnight + 30 * MIN); // 30 min today
    engine.tick();
    expect(hits).toHaveLength(0);
  });

  test('fires again the next day', () => {
    setLimit(db, { targetType: 'app', target: 'a', dailyMs: HOUR, action: 'notify' });
    addSession('a', start - 2 * HOUR, start);
    engine.tick();
    expect(hits).toHaveLength(1);

    const tomorrow = new Date(2026, 0, 6, 10, 0).getTime();
    addSession('a', tomorrow - 2 * HOUR, tomorrow);
    clock = tomorrow;
    engine.tick();
    expect(hits).toHaveLength(2);
  });

  test('overlay limits re-appear while the app is still in use, notify limits do not', () => {
    setLimit(db, { targetType: 'app', target: 'a', dailyMs: HOUR, action: 'overlay' });
    setLimit(db, { targetType: 'app', target: 'b', dailyMs: HOUR, action: 'notify' });
    addSession('a', start - 2 * HOUR, start);
    addSession('b', start - 2 * HOUR, start);
    state.currentAppId = 'a';
    engine.tick();
    expect(hits).toHaveLength(2);

    run(4 * MIN);
    expect(hits).toHaveLength(2); // inside the repeat window
    run(2 * MIN);
    expect(hits.map((h) => h.action)).toEqual(['overlay', 'notify', 'overlay']);

    state.currentAppId = 'other';
    run(10 * MIN);
    expect(hits).toHaveLength(3); // not in use: stays quiet
  });

  test('category and domain targets', () => {
    setLimit(db, { targetType: 'category', target: 'Browsing', dailyMs: HOUR, action: 'notify' });
    setLimit(db, {
      targetType: 'domain',
      target: 'youtube.com',
      dailyMs: 30 * MIN,
      action: 'notify',
    });
    addSession('org.mozilla.firefox', start - 2 * HOUR, start);
    db.query('INSERT INTO web_sessions (domain, start_ts, end_ts) VALUES (?, ?, ?)').run(
      'youtube.com',
      start - HOUR,
      start,
    );
    engine.tick();
    expect(hits).toHaveLength(2);
  });

  test('a schedule limits enforcement to its window', () => {
    // Window 13:00-17:00 while it's 10:00
    setLimit(db, {
      targetType: 'app',
      target: 'a',
      dailyMs: HOUR,
      action: 'notify',
      schedule: '13:00-17:00',
    });
    addSession('a', start - 2 * HOUR, start);
    engine.tick();
    expect(hits).toHaveLength(0);

    clock = new Date(2026, 0, 5, 13, 30).getTime();
    engine.tick();
    expect(hits).toHaveLength(1);
  });
});

describe('break reminders', () => {
  beforeEach(() => {
    settings = {
      ...settings,
      breakRemindersEnabled: true,
      breakEveryMinutes: 50,
      breakLengthMinutes: 5,
    };
  });

  test('reminds after continuous activity, then no more often than every 10 minutes', () => {
    run(49 * MIN);
    expect(reminders).toHaveLength(0);
    run(2 * MIN);
    expect(reminders).toHaveLength(1);
    expect(reminders[0]).toMatchObject({ kind: 'break' });
    expect(notes[0]?.body).toContain('50 min');

    run(8 * MIN);
    expect(reminders).toHaveLength(1);
    run(3 * MIN);
    expect(reminders).toHaveLength(2);
  });

  test('a real break resets the clock; a short one does not', () => {
    run(40 * MIN);
    engine.onTrackerEvent({ type: 'idle', at: clock });
    clock += 2 * MIN; // too short
    engine.onTrackerEvent({ type: 'active', at: clock });
    run(9 * MIN);
    expect(reminders).toHaveLength(1); // 40 + 2 + 9 = 51 min since the start

    reminders.length = 0;
    engine.onTrackerEvent({ type: 'idle', at: clock });
    clock += 6 * MIN; // a proper break
    engine.onTrackerEvent({ type: 'active', at: clock });
    run(40 * MIN);
    expect(reminders).toHaveLength(0);
  });

  test('a suspend counts as a break', () => {
    run(45 * MIN);
    clock += 3 * HOUR; // laptop asleep
    engine.tick();
    run(40 * MIN);
    expect(reminders).toHaveLength(0);
  });

  test('stays quiet while idle, paused or disabled', () => {
    state.idle = true;
    run(2 * HOUR);
    expect(reminders).toHaveLength(0);

    state.idle = false;
    settings = { ...settings, breakRemindersEnabled: false };
    run(2 * HOUR);
    expect(reminders).toHaveLength(0);
  });
});

describe('downtime', () => {
  beforeEach(() => {
    settings = { ...settings, downtimeEnabled: true, downtimeStart: '22:00', downtimeEnd: '07:00' };
    state.currentAppId = 'a';
  });

  test('nudges inside a window that wraps midnight, at most every 15 minutes', () => {
    clock = new Date(2026, 0, 5, 23, 0).getTime();
    engine.tick();
    expect(reminders).toEqual([{ kind: 'downtime', message: expect.stringContaining('07:00') }]);
    run(10 * MIN);
    expect(reminders).toHaveLength(1);
    run(6 * MIN);
    expect(reminders).toHaveLength(2);
  });

  test('is silent outside the window and when nobody is using the computer', () => {
    engine.tick(); // 10:00
    expect(reminders).toHaveLength(0);

    clock = new Date(2026, 0, 5, 23, 0).getTime();
    state.currentAppId = null;
    engine.tick();
    expect(reminders).toHaveLength(0);
  });
});

describe('focus mode', () => {
  test('nudges when a distracting app takes focus, rate limited, and announces the end', () => {
    addSession('com.spotify.Client', start - MIN, start); // categorised as Entertainment (distracting)
    const until = engine.startFocusMode(25);
    expect(until).toBe(start + 25 * MIN);
    expect(engine.focusMode()).toEqual({ active: true, until });

    engine.onTrackerEvent({ type: 'focus', appId: 'org.gnome.Ptyxis', since: clock }); // productive: fine
    expect(reminders).toHaveLength(0);

    engine.onTrackerEvent({ type: 'focus', appId: 'com.spotify.Client', since: clock });
    expect(reminders).toHaveLength(1);
    engine.onTrackerEvent({ type: 'focus', appId: 'com.spotify.Client', since: clock });
    expect(reminders).toHaveLength(1); // within the minute

    run(25 * MIN);
    expect(engine.focusMode().active).toBe(false);
    expect(reminders.at(-1)?.message).toContain('finished');
  });

  test('does nothing when focus mode is off', () => {
    addSession('com.spotify.Client', start - MIN, start);
    engine.onTrackerEvent({ type: 'focus', appId: 'com.spotify.Client', since: clock });
    expect(reminders).toHaveLength(0);
  });
});

test('formatDuration', () => {
  expect(formatDuration(30 * MIN)).toBe('30 min');
  expect(formatDuration(HOUR)).toBe('1 h');
  expect(formatDuration(HOUR + 5 * MIN)).toBe('1 h 5 min');
});
