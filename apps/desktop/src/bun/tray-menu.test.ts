import { describe, expect, test } from 'bun:test';
import type { TrackerStatus } from '@screentime/shared';
import { buildTrayMenu, parseTrayAction, trayTitle } from './tray-menu';

const status = (over: Partial<TrackerStatus> = {}): TrackerStatus => ({
  paused: false,
  resumeAt: null,
  idle: false,
  currentAppId: null,
  since: null,
  focusMode: { active: false, until: null },
  ...over,
});

const actions = (items: ReturnType<typeof buildTrayMenu>) =>
  items.flatMap((i) => (i.type === 'normal' ? [i.action] : []));

describe('buildTrayMenu', () => {
  test('offers pause and focus when tracking normally', () => {
    expect(actions(buildTrayMenu(status(), true))).toEqual([
      'noop',
      'widget',
      'open',
      'pause:15',
      'pause:60',
      'focus:25',
      'quit',
    ]);
  });

  test('offers resume while paused, and stop while in focus mode', () => {
    expect(actions(buildTrayMenu(status({ paused: true }), true))).toContain('resume');
    expect(actions(buildTrayMenu(status({ paused: true }), true))).not.toContain('pause:15');
    expect(
      actions(buildTrayMenu(status({ focusMode: { active: true, until: 1 } }), true)),
    ).toContain('focus-stop');
  });

  test('disables daemon controls, but not open/quit, when the daemon is down', () => {
    const items = buildTrayMenu(undefined, false);
    const byAction = Object.fromEntries(
      items.flatMap((i) => (i.type === 'normal' ? [[i.action, i.enabled]] : [])),
    );
    expect(byAction['pause:15']).toBe(false);
    expect(byAction['focus:25']).toBe(false);
    expect(byAction.open).toBeUndefined(); // enabled by default
    expect(byAction.quit).toBeUndefined();
  });
});

describe('status rows', () => {
  const first = (items: ReturnType<typeof buildTrayMenu>) =>
    items[0]?.type === 'normal' ? items[0] : undefined;

  test('describe what is happening, and are disabled', () => {
    const label = (st: TrackerStatus | undefined, connected: boolean, info = {}) =>
      first(buildTrayMenu(st, connected, info))?.label;
    expect(label(status(), true, { appName: 'Firefox' })).toBe('Tracking  Firefox');
    expect(label(status({ paused: true }), true, { resumeLabel: '15:30' })).toBe(
      'Paused until 15:30',
    );
    expect(label(status({ idle: true }), true)).toBe('Idle');
    expect(label(status({ focusMode: { active: true, until: 1 } }), true)).toBe('Focus mode on');
    expect(label(undefined, false)).toBe('Daemon not running');
    expect(first(buildTrayMenu(status(), true))?.enabled).toBe(false);
  });

  test('show today only while connected', () => {
    const rows = (connected: boolean) =>
      buildTrayMenu(status(), connected, { todayLabel: '4h 47m' }).flatMap((i) =>
        i.type === 'normal' ? [i.label] : [],
      );
    expect(rows(true)).toContain('Today  4h 47m');
    expect(rows(false)).not.toContain('Today  4h 47m');
  });
});

describe('parseTrayAction', () => {
  test('parses every action the menu can produce', () => {
    for (const item of [
      ...buildTrayMenu(status(), true),
      ...buildTrayMenu(status({ paused: true, focusMode: { active: true, until: 1 } }), true),
    ]) {
      if (item.type === 'normal' && item.enabled !== false) {
        expect(parseTrayAction(item.action)).toBeDefined();
      }
    }
    expect(parseTrayAction('widget')).toEqual({ kind: 'widget' });
    expect(parseTrayAction('noop')).toBeUndefined();
    expect(parseTrayAction('pause:15')).toEqual({ kind: 'pause', minutes: 15 });
    expect(parseTrayAction('focus:25')).toEqual({ kind: 'focus', minutes: 25 });
  });

  test('rejects unknown, empty and zero-minute actions', () => {
    expect(parseTrayAction(undefined)).toBeUndefined();
    expect(parseTrayAction('')).toBeUndefined();
    expect(parseTrayAction('pause:0')).toBeUndefined();
    expect(parseTrayAction('pause:abc')).toBeUndefined();
    expect(parseTrayAction('rm -rf')).toBeUndefined();
  });
});

test('trayTitle reflects the state', () => {
  expect(trayTitle(undefined, false)).toContain('not running');
  expect(trayTitle(status({ paused: true }), true)).toContain('paused');
  expect(trayTitle(status({ idle: true }), true)).toContain('idle');
  expect(trayTitle(status(), true)).toContain('tracking');
});
