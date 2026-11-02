import { describe, expect, test } from 'bun:test';
import { pickPlayer } from './mediaRules.js';

const p = (name, status, changedAt, title = 'Song') => ({ name, status, changedAt, title });

describe('pickPlayer', () => {
  test('nothing loaded, nothing shown', () => {
    expect(pickPlayer([])).toBeNull();
    expect(pickPlayer([p('a', 'Stopped', 1, '')])).toBeNull();
  });

  test('a playing player wins over a paused one that changed later', () => {
    expect(pickPlayer([p('spotify', 'Playing', 1), p('chrome', 'Paused', 9)]).name).toBe('spotify');
  });

  test('of two playing, the one that started most recently', () => {
    expect(pickPlayer([p('spotify', 'Playing', 1), p('chrome', 'Playing', 5)]).name).toBe('chrome');
  });

  test('nothing playing: the most recently paused', () => {
    expect(pickPlayer([p('spotify', 'Paused', 3), p('chrome', 'Paused', 2)]).name).toBe('spotify');
  });
});
