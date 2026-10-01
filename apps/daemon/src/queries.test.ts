import { afterEach, beforeEach, describe, expect, test } from 'bun:test';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { openDb } from '@screentime/db';
import { ensureApp } from './apps';
import {
  deleteLimit,
  exportData,
  listApps,
  listLimits,
  setAppCategory,
  setLimit,
  timeline,
  usageFor,
  usageSummary,
  usageWeb,
  wipeData,
} from './queries';
import { getSettings, patchSettings } from './settings-store';

let dir: string;
let db: ReturnType<typeof openDb>;

const HOUR = 3_600_000;
const at = (y: number, mo: number, d: number, h = 0, mi = 0) =>
  new Date(y, mo - 1, d, h, mi).getTime();

function addSession(appId: string, start: number, end: number) {
  const id = ensureApp(db, appId, () => undefined);
  db.query(
    "INSERT INTO sessions (app_id, start_ts, end_ts, source) VALUES (?, ?, ?, 'desktop')",
  ).run(id, start, end);
}

beforeEach(() => {
  dir = mkdtempSync(join(tmpdir(), 'screentime-q-'));
  db = openDb(join(dir, 't.db'));
});
afterEach(() => {
  db.close();
  rmSync(dir, { recursive: true, force: true });
});

describe('usageSummary', () => {
  test('groups by app and clips sessions to the requested window', () => {
    addSession('org.mozilla.firefox', at(2026, 1, 1, 9), at(2026, 1, 1, 11)); // 2h
    addSession('org.gnome.Ptyxis', at(2026, 1, 1, 11), at(2026, 1, 1, 12)); // 1h
    // window covers only 10:00-11:30: 1h of firefox, 30m of terminal
    const rows = usageSummary(db, {
      from: at(2026, 1, 1, 10),
      to: at(2026, 1, 1, 11, 30),
      groupBy: 'app',
    });
    expect(rows).toEqual([
      { key: 'org.mozilla.firefox', ms: HOUR },
      { key: 'org.gnome.Ptyxis', ms: HOUR / 2 },
    ]);
  });

  test('groups by category with an Uncategorized bucket', () => {
    addSession('org.mozilla.firefox', at(2026, 1, 1, 9), at(2026, 1, 1, 10));
    addSession('com.unknown.Thing', at(2026, 1, 1, 10), at(2026, 1, 1, 12));
    const rows = usageSummary(db, {
      from: at(2026, 1, 1),
      to: at(2026, 1, 2),
      groupBy: 'category',
    });
    expect(rows).toEqual([
      { key: 'Uncategorized', ms: 2 * HOUR },
      { key: 'Browsing', ms: HOUR },
    ]);
  });

  test('splits a session across midnight when grouping by day', () => {
    addSession('a', at(2026, 1, 1, 23, 50), at(2026, 1, 2, 0, 10));
    const rows = usageSummary(db, { from: at(2026, 1, 1), to: at(2026, 1, 3), groupBy: 'day' });
    expect(rows).toEqual([
      { key: '2026-01-01', ms: 10 * 60_000 },
      { key: '2026-01-02', ms: 10 * 60_000 },
    ]);
  });

  test('splits a session across hours when grouping by hour of day', () => {
    addSession('a', at(2026, 1, 1, 9, 45), at(2026, 1, 1, 10, 15));
    const rows = usageSummary(db, { from: at(2026, 1, 1), to: at(2026, 1, 2), groupBy: 'hour' });
    expect(rows).toEqual([
      { key: '09', ms: 15 * 60_000 },
      { key: '10', ms: 15 * 60_000 },
    ]);
  });
});

describe('timeline', () => {
  test('returns sessions overlapping a local date, with app IDs and no empty titles', () => {
    addSession('a', at(2026, 1, 1, 9), at(2026, 1, 1, 10));
    addSession('b', at(2026, 1, 2, 9), at(2026, 1, 2, 10));
    const sessions = timeline(db, '2026-01-01');
    expect(sessions).toHaveLength(1);
    expect(sessions[0]).toMatchObject({ appId: 'a', source: 'desktop' });
    expect(sessions[0]).not.toHaveProperty('title');
  });

  test('rejects malformed dates', () => {
    expect(() => timeline(db, 'yesterday')).toThrow('invalid date');
  });
});

describe('web usage and usageFor', () => {
  test('sums web sessions by domain and per-target usage by type', () => {
    db.query('INSERT INTO web_sessions (domain, start_ts, end_ts) VALUES (?, ?, ?)').run(
      'youtube.com',
      at(2026, 1, 1, 9),
      at(2026, 1, 1, 10),
    );
    addSession('org.mozilla.firefox', at(2026, 1, 1, 9), at(2026, 1, 1, 10));
    const from = at(2026, 1, 1);
    const to = at(2026, 1, 2);

    expect(usageWeb(db, from, to)).toEqual([{ key: 'youtube.com', ms: HOUR }]);
    expect(usageFor(db, 'domain', 'youtube.com', from, to)).toBe(HOUR);
    expect(usageFor(db, 'app', 'org.mozilla.firefox', from, to)).toBe(HOUR);
    expect(usageFor(db, 'category', 'Browsing', from, to)).toBe(HOUR);
    expect(usageFor(db, 'app', 'nothing', from, to)).toBe(0);
  });
});

describe('apps and categories', () => {
  test('setAppCategory validates the category and reports unknown apps', () => {
    ensureApp(db, 'x', () => undefined);
    expect(setAppCategory(db, 'x', 9999)).toBe(false);
    expect(setAppCategory(db, 'missing', null)).toBe(false);
    expect(setAppCategory(db, 'x', 1)).toBe(true);
    expect(listApps(db).find((a) => a.appId === 'x')?.categoryId).toBe(1);
  });
});

describe('limits', () => {
  const base = {
    targetType: 'app',
    target: 'org.mozilla.firefox',
    dailyMs: HOUR,
    action: 'notify',
  } as const;

  test('insert, update by target, update by id, delete', () => {
    const created = setLimit(db, base);
    expect(created.id).toBeDefined();

    const updated = setLimit(db, { ...base, dailyMs: 2 * HOUR }); // same target: upsert
    expect(updated.id).toBe(created.id);
    expect(listLimits(db)).toHaveLength(1);
    expect(listLimits(db)[0]?.dailyMs).toBe(2 * HOUR);

    const byId = setLimit(db, {
      ...base,
      id: created.id,
      action: 'overlay',
      schedule: '09:00-17:00',
    });
    expect(byId).toMatchObject({ action: 'overlay', schedule: '09:00-17:00' });

    expect(deleteLimit(db, created.id as number)).toBe(true);
    expect(deleteLimit(db, created.id as number)).toBe(false);
  });

  test('updating a missing id throws', () => {
    expect(() => setLimit(db, { ...base, id: 42 })).toThrow('no limit with id 42');
  });
});

describe('export and wipe', () => {
  test('CSV quotes cells and defuses formula injection', () => {
    addSession('=cmd|calc', at(2026, 1, 1, 9), at(2026, 1, 1, 10));
    addSession('a,b', at(2026, 1, 1, 10), at(2026, 1, 1, 11));
    const { content, filename } = exportData(db, 'csv', undefined, undefined, at(2026, 3, 4));
    expect(filename).toBe('screentime-2026-03-04.csv');
    expect(content).toContain("desktop,'=cmd|calc,");
    expect(content).toContain('desktop,"a,b",');
  });

  test('JSON export has durations', () => {
    addSession('a', at(2026, 1, 1, 9), at(2026, 1, 1, 10));
    const parsed = JSON.parse(exportData(db, 'json').content);
    expect(parsed[0]).toMatchObject({ source: 'desktop', target: 'a', durationMs: HOUR });
  });

  test('wipe removes usage but keeps settings and limits unless asked', () => {
    addSession('a', at(2026, 1, 1, 9), at(2026, 1, 1, 10));
    setLimit(db, { targetType: 'app', target: 'a', dailyMs: HOUR, action: 'notify' });
    patchSettings(db, { captureTitles: true });

    wipeData(db, false);
    expect(db.query('SELECT COUNT(*) AS n FROM sessions').get()).toEqual({ n: 0 });
    expect(listLimits(db)).toHaveLength(1);
    expect(getSettings(db).captureTitles).toBe(true);

    wipeData(db, true);
    expect(listLimits(db)).toHaveLength(0);
    expect(getSettings(db).captureTitles).toBe(false);
    expect(db.query('SELECT COUNT(*) AS n FROM apps').get()).toEqual({ n: 0 });
  });
});

describe('settings store', () => {
  test('defaults, patching, validation and corrupt-value fallback', () => {
    expect(getSettings(db).captureTitles).toBe(false);
    expect(patchSettings(db, { idleThresholdMinutes: 10 }).idleThresholdMinutes).toBe(10);
    expect(() => patchSettings(db, { idleThresholdMinutes: 0 })).toThrow();
    expect(() => patchSettings(db, { nope: 1 } as never)).toThrow();

    db.query(
      "INSERT OR REPLACE INTO settings (key, value) VALUES ('idleThresholdMinutes', '\"oops\"')",
    ).run();
    db.query(
      "INSERT OR REPLACE INTO settings (key, value) VALUES ('captureTitles', '{bad json')",
    ).run();
    const s = getSettings(db);
    expect(s.idleThresholdMinutes).toBe(3);
    expect(s.captureTitles).toBe(false);
  });
});
