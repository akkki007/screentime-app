import { afterEach, describe, expect, test } from 'bun:test';
import { mkdtempSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { openDb } from '@screentime/db';
import { backfillApps, ensureApp } from './apps';
import { lookupDesktopEntry, parseDesktopEntry } from './desktop-entries';

const dirs: string[] = [];
function tempDir(): string {
  const dir = mkdtempSync(join(tmpdir(), 'screentime-test-'));
  dirs.push(dir);
  return dir;
}

afterEach(() => {
  for (const dir of dirs.splice(0)) rmSync(dir, { recursive: true, force: true });
});

describe('desktop entries', () => {
  test('parses the unlocalised Name and Icon from the Desktop Entry group only', () => {
    const info = parseDesktopEntry(`[Desktop Entry]
Name=Firefox
Name[de]=Feuerfuchs
Icon=firefox
[Desktop Action new-window]
Name=New Window
Icon=other
`);
    expect(info).toEqual({ name: 'Firefox', icon: 'firefox' });
  });

  test('finds an entry in the search path and ignores path traversal', () => {
    const dir = tempDir();
    writeFileSync(join(dir, 'org.example.App.desktop'), '[Desktop Entry]\nName=Example\n');
    expect(lookupDesktopEntry('org.example.App', [dir])).toEqual({ name: 'Example' });
    expect(lookupDesktopEntry('missing', [dir])).toBeUndefined();
    expect(lookupDesktopEntry('../org.example.App', [join(dir, 'sub')])).toBeUndefined();
  });
});

describe('ensureApp', () => {
  const dbPath = () => join(tempDir(), 'test.db');

  test('creates an app with name, icon and a default category', () => {
    const db = openDb(dbPath());
    const id = ensureApp(db, 'org.mozilla.firefox', () => ({ name: 'Firefox', icon: 'firefox' }));

    const row = db
      .query(
        'SELECT apps.name, apps.icon, categories.name AS category FROM apps JOIN categories ON categories.id = apps.category_id WHERE apps.id = ?',
      )
      .get(id);
    expect(row).toEqual({ name: 'Firefox', icon: 'firefox', category: 'Browsing' });
    db.close();
  });

  test('is idempotent and never overwrites a user-chosen category', () => {
    const db = openDb(dbPath());
    const id = ensureApp(db, 'org.mozilla.firefox', () => undefined);
    const other = db.query("SELECT id FROM categories WHERE name = 'Entertainment'").get() as {
      id: number;
    };
    db.query('UPDATE apps SET category_id = ? WHERE id = ?').run(other.id, id);

    expect(ensureApp(db, 'org.mozilla.firefox', () => undefined)).toBe(id);
    const row = db.query('SELECT category_id FROM apps WHERE id = ?').get(id) as {
      category_id: number;
    };
    expect(row.category_id).toBe(other.id);
    db.close();
  });

  test('leaves unknown apps uncategorised, and backfill fills gaps later', () => {
    const db = openDb(dbPath());
    const id = ensureApp(db, 'com.unknown.Thing', () => undefined);
    expect(
      (
        db.query('SELECT category_id FROM apps WHERE id = ?').get(id) as {
          category_id: number | null;
        }
      ).category_id,
    ).toBeNull();

    db.query('INSERT INTO apps (app_id) VALUES (?)').run('org.gnome.Ptyxis');
    backfillApps(db, () => ({ name: 'Terminal' }));
    const row = db
      .query(
        "SELECT apps.name, categories.name AS category FROM apps JOIN categories ON categories.id = apps.category_id WHERE app_id = 'org.gnome.Ptyxis'",
      )
      .get();
    expect(row).toEqual({ name: 'Terminal', category: 'Development' });
    db.close();
  });
});
