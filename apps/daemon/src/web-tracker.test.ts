import { afterEach, beforeEach, describe, expect, test } from 'bun:test';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { openDb } from '@screentime/db';
import { WebTracker } from './web-tracker';

let dir: string;
let db: ReturnType<typeof openDb>;
let clock: number;
let web: WebTracker;

const rows = () =>
  db.query('SELECT domain, start_ts, end_ts FROM web_sessions ORDER BY id').all() as {
    domain: string;
    start_ts: number;
    end_ts: number;
  }[];

beforeEach(() => {
  dir = mkdtempSync(join(tmpdir(), 'screentime-web-'));
  db = openDb(join(dir, 't.db'));
  clock = 1_000_000;
  web = new WebTracker(db, () => clock);
});
afterEach(() => {
  db.close();
  rmSync(dir, { recursive: true, force: true });
});

describe('WebTracker', () => {
  test('opens a session for the reported domain and extends it on heartbeats', () => {
    web.handle({ domain: 'a.com', active: true }, true);
    clock += 5_000;
    web.sync(true);
    expect(rows()).toEqual([{ domain: 'a.com', start_ts: 1_000_000, end_ts: 1_005_000 }]);
  });

  test('switching domains closes one session and opens the next', () => {
    web.handle({ domain: 'a.com', active: true }, true);
    clock += 4_000;
    web.handle({ domain: 'b.com', active: true }, true);
    expect(rows()).toEqual([
      { domain: 'a.com', start_ts: 1_000_000, end_ts: 1_004_000 },
      { domain: 'b.com', start_ts: 1_004_000, end_ts: 1_004_000 },
    ]);
  });

  test('does not count time while no browser is focused, and resumes on return', () => {
    web.handle({ domain: 'a.com', active: true }, true);
    clock += 5_000;
    web.sync(true);
    clock += 60_000;
    web.sync(false); // user went to the terminal
    clock += 60_000;
    web.sync(false);
    expect(rows()[0]?.end_ts).toBe(1_005_000);

    web.sync(true); // back in the browser, same tab: no new extension message
    expect(rows()).toHaveLength(2);
    expect(rows()[1]).toMatchObject({ domain: 'a.com', start_ts: clock });
  });

  test('active:false ends tracking until a new domain is reported', () => {
    web.handle({ domain: 'a.com', active: true }, true);
    clock += 3_000;
    web.handle({ active: false }, true);
    clock += 3_000;
    web.sync(true);
    expect(rows()).toHaveLength(1);
    expect(rows()[0]?.end_ts).toBe(1_003_000); // on the page until the browser reported leaving
  });

  test('a reported domain is not counted until a browser is focused', () => {
    web.handle({ domain: 'a.com', active: true }, false);
    expect(rows()).toHaveLength(0);
  });

  test('closeAt ends the session where idleness began, not later', () => {
    web.handle({ domain: 'a.com', active: true }, true);
    for (let i = 0; i < 4; i++) {
      clock += 5_000;
      web.sync(true);
    }
    web.closeAt(1_000_000 + 8_000);
    expect(rows()[0]?.end_ts).toBe(1_008_000);
  });

  test('a long heartbeat gap splits the session', () => {
    web.handle({ domain: 'a.com', active: true }, true);
    clock += 5_000;
    web.sync(true);
    clock += 8 * 3_600_000;
    web.sync(true);
    expect(rows()).toHaveLength(2);
    expect(rows()[0]?.end_ts).toBe(1_005_000);
  });
});
