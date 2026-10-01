import { describe, expect, test } from 'bun:test';
import {
  addDays,
  appLabel,
  dateKey,
  formatDuration,
  formatShort,
  parseDurationInput,
  percent,
  startOfDay,
} from './format';

const MIN = 60_000;
const HOUR = 60 * MIN;

describe('formatDuration', () => {
  test('covers zero, sub-minute, minutes, hours', () => {
    expect(formatDuration(0)).toBe('0 min');
    expect(formatDuration(-5)).toBe('0 min');
    expect(formatDuration(20_000)).toBe('<1 min');
    expect(formatDuration(45 * MIN)).toBe('45 min');
    expect(formatDuration(2 * HOUR)).toBe('2 h');
    expect(formatDuration(2 * HOUR + 5 * MIN)).toBe('2 h 5 min');
  });

  test('rounds to the nearest minute', () => {
    expect(formatDuration(89 * 1000)).toBe('1 min');
    expect(formatDuration(91 * 1000)).toBe('2 min');
  });
});

test('formatShort', () => {
  expect(formatShort(0)).toBe('0m');
  expect(formatShort(10_000)).toBe('<1m');
  expect(formatShort(HOUR + 30 * MIN)).toBe('1h 30m');
  expect(formatShort(3 * HOUR)).toBe('3h');
});

describe('dates', () => {
  test('startOfDay and addDays are local-midnight aligned', () => {
    const noon = new Date(2026, 0, 5, 12, 30).getTime();
    expect(startOfDay(noon)).toBe(new Date(2026, 0, 5).getTime());
    expect(addDays(noon, 1)).toBe(new Date(2026, 0, 6).getTime());
    expect(addDays(noon, -5)).toBe(new Date(2025, 11, 31).getTime());
  });

  test('addDays survives a month boundary and dateKey is zero padded', () => {
    expect(dateKey(addDays(new Date(2026, 0, 31, 9).getTime(), 1))).toBe('2026-02-01');
    expect(dateKey(new Date(2026, 2, 4).getTime())).toBe('2026-03-04');
  });
});

describe('appLabel', () => {
  test('prefers the desktop-entry name', () => {
    expect(appLabel('org.mozilla.firefox', 'Firefox')).toBe('Firefox');
  });
  test('derives a readable name from IDs', () => {
    expect(appLabel('org.gnome.Ptyxis')).toBe('Ptyxis');
    expect(appLabel('firefox_firefox')).toBe('Firefox');
    expect(appLabel('vpn-unlimited')).toBe('Vpn-unlimited');
  });
});

test('percent', () => {
  expect(percent(1, 4)).toBe(25);
  expect(percent(1, 3)).toBe(33);
  expect(percent(5, 0)).toBe(0);
});

describe('parseDurationInput', () => {
  test('accepts common spellings', () => {
    expect(parseDurationInput('90')).toBe(90 * MIN);
    expect(parseDurationInput('45m')).toBe(45 * MIN);
    expect(parseDurationInput('1h')).toBe(HOUR);
    expect(parseDurationInput('1h30')).toBe(90 * MIN);
    expect(parseDurationInput('1h 30m')).toBe(90 * MIN);
    expect(parseDurationInput('1:30')).toBe(90 * MIN);
    expect(parseDurationInput('1.5h')).toBe(90 * MIN);
  });

  test('rejects empty, zero and nonsense', () => {
    expect(parseDurationInput('')).toBeUndefined();
    expect(parseDurationInput('0')).toBeUndefined();
    expect(parseDurationInput('soon')).toBeUndefined();
    expect(parseDurationInput('-5')).toBeUndefined();
  });
});
