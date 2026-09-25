import { afterEach, describe, expect, test } from 'bun:test';
import { existsSync, rmSync } from 'node:fs';
import { openDb } from './client';

const TEST_DB_PATH = `${import.meta.dir}/.test-screentime.db`;

afterEach(() => {
  for (const suffix of ['', '-wal', '-shm']) {
    const path = `${TEST_DB_PATH}${suffix}`;
    if (existsSync(path)) rmSync(path);
  }
});

describe('openDb', () => {
  test('applies migrations and creates the expected tables', () => {
    const db = openDb(TEST_DB_PATH);

    const tables = db
      .query("SELECT name FROM sqlite_master WHERE type = 'table' ORDER BY name")
      .all()
      .map((row) => (row as { name: string }).name);

    expect(tables).toEqual(['apps', 'categories', 'limits', 'sessions', 'web_sessions']);
    db.close();
  });

  test('is idempotent across repeated opens', () => {
    openDb(TEST_DB_PATH).close();
    const db = openDb(TEST_DB_PATH);
    const version = (db.query('PRAGMA user_version;').get() as { user_version: number })
      .user_version;
    expect(version).toBe(1);
    db.close();
  });
});
