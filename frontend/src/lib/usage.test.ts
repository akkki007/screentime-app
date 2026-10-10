import { describe, expect, test } from 'bun:test';
import { fillDays, fillHours, latestOnly, sum } from './usage';

describe('fillDays', () => {
  test('zero-fills days with no usage, in order', () => {
    const start = new Date(2026, 0, 30).getTime();
    const rows = fillDays(start, 4, [
      { key: '2026-01-31', ms: 5 },
      { key: '2026-02-02', ms: 9 },
    ]);
    expect(rows).toEqual([
      { key: '2026-01-30', ms: 0 },
      { key: '2026-01-31', ms: 5 },
      { key: '2026-02-01', ms: 0 },
      { key: '2026-02-02', ms: 9 },
    ]);
  });
});

test('fillHours returns 24 slots keyed by zero-padded hour', () => {
  const hours = fillHours([
    { key: '09', ms: 100 },
    { key: '23', ms: 7 },
  ]);
  expect(hours).toHaveLength(24);
  expect(hours[9]).toBe(100);
  expect(hours[23]).toBe(7);
  expect(hours[0]).toBe(0);
});

test('sum', () => {
  expect(sum([])).toBe(0);
  expect(
    sum([
      { key: 'a', ms: 2 },
      { key: 'b', ms: 3 },
    ]),
  ).toBe(5);
});

test('latestOnly only honours the newest ticket', () => {
  const guard = latestOnly();
  const first = guard.next();
  const second = guard.next();
  expect(guard.isCurrent(first)).toBe(false);
  expect(guard.isCurrent(second)).toBe(true);
});
