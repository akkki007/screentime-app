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

    expect(tables).toEqual([
      'apps',
      'categories',
      'limits',
      'sessions',
      'settings',
      'web_sessions',
    ]);
    db.close();
  });

  test('is idempotent across repeated opens', () => {
    openDb(TEST_DB_PATH).close();
    const db = openDb(TEST_DB_PATH);
    const version = (db.query('PRAGMA user_version;').get() as { user_version: number })
      .user_version;
    expect(version).toBe(4);
    db.close();
  });

  test('seeds the built-in categories', () => {
    const db = openDb(TEST_DB_PATH);
    const names = db
      .query('SELECT name FROM categories ORDER BY id')
      .all()
      .map((row) => (row as { name: string }).name);
    expect(names).toContain('Development');
    expect(names).toContain('Other');
    db.close();
  });

  test('migration 0004 strips the .desktop suffix unless it would collide', () => {
    const db = openDb(TEST_DB_PATH);
    db.exec('PRAGMA user_version = 3');
    db.query('INSERT INTO apps (app_id) VALUES (?), (?), (?), (?)').run(
      'org.gnome.Ptyxis.desktop',
      'org.mozilla.firefox.desktop',
      'org.mozilla.firefox',
      'plain',
    );
    db.close();

    const reopened = openDb(TEST_DB_PATH);
    const ids = reopened
      .query('SELECT app_id FROM apps ORDER BY app_id')
      .all()
      .map((row) => (row as { app_id: string }).app_id);
    expect(ids).toEqual([
      'org.gnome.Ptyxis',
      'org.mozilla.firefox',
      'org.mozilla.firefox.desktop', // collides with the existing row: left alone
      'plain',
    ]);
    reopened.close();
  });
});
