import { Database } from 'bun:sqlite';
import { mkdirSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { dataDir } from '@screentime/shared';
import { MIGRATIONS } from './migrations';

/**
 * Opens (creating if needed) the app's SQLite database in WAL mode and
 * applies any migrations that haven't run yet, tracked via PRAGMA user_version.
 */
export function openDb(path = join(dataDir(), 'screentime.db')): Database {
  mkdirSync(dirname(path), { recursive: true });

  const db = new Database(path, { create: true });
  db.exec('PRAGMA journal_mode = WAL;');
  db.exec('PRAGMA foreign_keys = ON;');

  runMigrations(db);

  return db;
}

function runMigrations(db: Database): void {
  const currentVersion = (db.query('PRAGMA user_version;').get() as { user_version: number })
    .user_version;

  for (const { version, sql } of MIGRATIONS) {
    if (version <= currentVersion) continue;

    db.transaction(() => {
      db.exec(sql);
      db.exec(`PRAGMA user_version = ${version};`);
    })();
  }
}
