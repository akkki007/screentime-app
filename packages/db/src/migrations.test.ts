import { describe, expect, test } from 'bun:test';
import { readdirSync } from 'node:fs';
import { join } from 'node:path';
import { MIGRATIONS } from './migrations';

const dir = join(import.meta.dir, '..', 'migrations');

describe('embedded migrations', () => {
  test('every .sql file on disk is registered, in version order', () => {
    const onDisk = readdirSync(dir)
      .filter((f) => f.endsWith('.sql'))
      .sort();
    expect(MIGRATIONS.map((m) => m.name)).toEqual(onDisk);
  });

  test('versions are sequential from 1 and match the file name prefix', () => {
    MIGRATIONS.forEach((m, i) => {
      expect(m.version).toBe(i + 1);
      expect(Number.parseInt(m.name.split('_')[0] ?? '', 10)).toBe(m.version);
      expect(m.sql.trim().length).toBeGreaterThan(0);
    });
  });
});
