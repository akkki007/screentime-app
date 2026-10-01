import type { Database } from 'bun:sqlite';
import { DEFAULT_SETTINGS, SettingsPatchSchema, SettingsSchema } from '@screentime/shared';
import type { Settings, SettingsPatch } from '@screentime/shared';

/**
 * Reads settings, falling back to defaults per key: a corrupt or out-of-range
 * stored value must not stop the daemon from starting.
 */
export function getSettings(db: Database): Settings {
  const rows = db.query('SELECT key, value FROM settings').all() as {
    key: string;
    value: string;
  }[];
  const stored: Record<string, unknown> = {};
  for (const { key, value } of rows) {
    try {
      stored[key] = JSON.parse(value);
    } catch {
      // ignore unparseable value; default applies
    }
  }

  const result: Record<string, unknown> = { ...DEFAULT_SETTINGS };
  for (const key of Object.keys(DEFAULT_SETTINGS)) {
    if (!(key in stored)) continue;
    const candidate = SettingsPatchSchema.safeParse({ [key]: stored[key] });
    if (candidate.success) result[key] = stored[key];
  }
  return SettingsSchema.parse(result);
}

/** Validates and persists a partial update, returning the full settings. */
export function patchSettings(db: Database, patch: SettingsPatch): Settings {
  const valid = SettingsPatchSchema.parse(patch);
  const upsert = db.query(
    'INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value',
  );

  db.transaction(() => {
    for (const [key, value] of Object.entries(valid)) {
      if (value !== undefined) upsert.run(key, JSON.stringify(value));
    }
  })();

  return getSettings(db);
}
