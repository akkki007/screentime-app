import { Database } from 'bun:sqlite';
import { mkdirSync, readFileSync, readdirSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { dataDir } from '@screentime/shared';

const MIGRATIONS_DIR = join(dirname(new URL(import.meta.url).pathname), '..', 'migrations');

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

  const files = readdirSync(MIGRATIONS_DIR)
    .filter((f) => f.endsWith('.sql'))
    .sort();

  for (const file of files) {
    const version = Number.parseInt(file.split('_')[0] ?? '0', 10);
    if (version <= currentVersion) continue;

    const sql = readFileSync(join(MIGRATIONS_DIR, file), 'utf8');
    db.transaction(() => {
      db.exec(sql);
      db.exec(`PRAGMA user_version = ${version};`);
    })();
  }
}
