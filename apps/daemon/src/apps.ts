import type { Database } from 'bun:sqlite';
import { defaultCategoryFor } from '@screentime/shared';
import { type DesktopEntryInfo, lookupDesktopEntry } from './desktop-entries';

type Lookup = (appId: string) => DesktopEntryInfo | undefined;

function categoryIdByName(db: Database, name: string): number | null {
  const row = db.query('SELECT id FROM categories WHERE name = ?').get(name) as {
    id: number;
  } | null;
  return row?.id ?? null;
}

/**
 * Returns the row ID for an app, creating it on first sight with its display
 * name, icon (from the .desktop file) and default category. Existing rows are
 * never touched, so user category overrides survive.
 */
export function ensureApp(
  db: Database,
  appId: string,
  lookup: Lookup = lookupDesktopEntry,
): number {
  const existing = db.query('SELECT id FROM apps WHERE app_id = ?').get(appId) as {
    id: number;
  } | null;
  if (existing) return existing.id;

  const entry = lookup(appId);
  const categoryName = defaultCategoryFor(appId);
  const categoryId = categoryName ? categoryIdByName(db, categoryName) : null;

  const result = db
    .query('INSERT INTO apps (app_id, name, icon, category_id) VALUES (?, ?, ?, ?)')
    .run(appId, entry?.name ?? null, entry?.icon ?? null, categoryId);
  return Number(result.lastInsertRowid);
}

/** Fills in names and categories for apps recorded before those rules existed. */
export function backfillApps(db: Database, lookup: Lookup = lookupDesktopEntry): void {
  const rows = db
    .query(
      'SELECT id, app_id, name, icon, category_id FROM apps WHERE name IS NULL OR category_id IS NULL',
    )
    .all() as {
    id: number;
    app_id: string;
    name: string | null;
    icon: string | null;
    category_id: number | null;
  }[];

  for (const row of rows) {
    const entry = row.name === null ? lookup(row.app_id) : undefined;
    const categoryName = row.category_id === null ? defaultCategoryFor(row.app_id) : undefined;
    const categoryId = categoryName ? categoryIdByName(db, categoryName) : null;

    db.query(
      'UPDATE apps SET name = COALESCE(name, ?), icon = COALESCE(icon, ?), category_id = COALESCE(category_id, ?) WHERE id = ?',
    ).run(entry?.name ?? null, entry?.icon ?? null, categoryId, row.id);
  }
}
