/// <reference path="./sql.d.ts" />
/**
 * Migrations embedded at build time. Reading `../migrations/*.sql` from disk
 * works in development but not inside a compiled binary (`bun build
 * --compile`), where there is no such directory, so each file is imported as
 * text. migrations.test.ts fails if a .sql file is added without registering
 * it here.
 */
import m0001 from '../migrations/0001_init.sql' with { type: 'text' };
import m0002 from '../migrations/0002_settings.sql' with { type: 'text' };
import m0003 from '../migrations/0003_default_categories.sql' with { type: 'text' };
import m0004 from '../migrations/0004_normalize_app_ids.sql' with { type: 'text' };

export type Migration = { version: number; name: string; sql: string };

export const MIGRATIONS: Migration[] = [
  { version: 1, name: '0001_init.sql', sql: m0001 },
  { version: 2, name: '0002_settings.sql', sql: m0002 },
  { version: 3, name: '0003_default_categories.sql', sql: m0003 },
  { version: 4, name: '0004_normalize_app_ids.sql', sql: m0004 },
];
