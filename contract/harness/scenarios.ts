/**
 * What the fixtures exercise. Each scenario runs against a fresh daemon with
 * the seed data, so scenarios are independent. `record.ts` turns these into
 * fixtures/<name>.json by recording what the current daemon answers.
 *
 * Times: the clock is frozen at NOW (Wed 2026-09-30 15:30, Asia/Kolkata).
 */
import type { FixtureEnv, Message } from './daemon';

export const NOW = 1790762400000;
export const ENV: FixtureEnv = { now: NOW, tz: 'Asia/Kolkata', seed: 'seed.sql' };

const MIN = 60_000;
const DAY = 24 * 60 * MIN;
/** Local midnight starting Sep 30 (today) in Asia/Kolkata. */
const TODAY = 1790706600000;

export type Step = Message | string;
export type Scenario = { name: string; description: string; steps: Step[] };

/** Builds requests with ids counting up from 1 within one scenario. */
function requests() {
  let id = 0;
  return {
    call: (method: string, params?: unknown): Message =>
      params === undefined
        ? { jsonrpc: '2.0', id: ++id, method }
        : { jsonrpc: '2.0', id: ++id, method, params },
    notify: (method: string, params?: unknown): Message => ({ jsonrpc: '2.0', method, params }),
  };
}

function scenario(
  name: string,
  description: string,
  build: (r: ReturnType<typeof requests>) => Step[],
): Scenario {
  return { name, description, steps: build(requests()) };
}

export const SCENARIOS: Scenario[] = [
  scenario(
    'protocol',
    'Version handshake, unknown methods and malformed input',
    ({ call, notify }) => [
      call('version', { major: 1 }),
      call('version', { major: 2 }),
      call('version'),
      call('no.such.method'),
      // Not JSON, not an object, no method: ignored without a response.
      'this is not json',
      '[1, 2, 3]',
      '{"jsonrpc":"2.0","id":99}',
      // A request without an id is a notification: handled, never answered.
      notify('tracker.status'),
      // String ids are echoed back as given.
      { jsonrpc: '2.0', id: 'abc', method: 'tracker.status' },
      // The connection still works after all of the above.
      call('version', { major: 1 }),
    ],
  ),

  scenario('usage', 'Read-only usage queries over the seed data', ({ call }) => [
    call('usage.summary', { from: TODAY, to: NOW, groupBy: 'app' }),
    call('usage.summary', { from: TODAY, to: NOW, groupBy: 'category' }),
    call('usage.summary', { from: TODAY, to: NOW, groupBy: 'hour' }),
    call('usage.summary', { from: TODAY - 6 * DAY, to: NOW, groupBy: 'day' }),
    call('usage.summary', { from: TODAY - 6 * DAY, to: NOW, groupBy: 'app' }),
    // A window that cuts sessions in half: only the overlap counts.
    call('usage.summary', {
      from: TODAY + 9 * 60 * MIN + 30 * MIN,
      to: TODAY + 10 * 60 * MIN,
      groupBy: 'app',
    }),
    // Empty and inverted ranges.
    call('usage.summary', { from: NOW, to: NOW, groupBy: 'app' }),
    call('usage.summary', { from: NOW, to: TODAY, groupBy: 'day' }),
    call('usage.timeline', { date: '2026-09-30' }),
    // Includes the session that started the evening before.
    call('usage.timeline', { date: '2026-09-29' }),
    call('usage.timeline', { date: '2026-01-01' }),
    call('usage.web', { from: TODAY, to: NOW }),
    call('usage.web', { from: TODAY - 6 * DAY, to: NOW }),
    call('apps.list'),
    call('categories.list'),
    call('limits.list'),
    call('settings.get'),
    call('tracker.status'),
  ]),

  scenario(
    'validation',
    'Malformed params are rejected with -32603 and a field: reason message',
    ({ call }) => [
      call('usage.summary', { from: TODAY, to: NOW }),
      call('usage.summary', { from: TODAY, to: NOW, groupBy: 'week' }),
      call('usage.summary', { from: 1.5, to: NOW, groupBy: 'app' }),
      call('usage.summary'),
      call('usage.timeline', {}),
      call('usage.timeline', { date: 'not-a-date' }),
      call('limits.set', { targetType: 'app', target: 'code', dailyMs: 0, action: 'notify' }),
      call('limits.set', {
        targetType: 'app',
        target: 'code',
        dailyMs: MIN,
        action: 'notify',
        schedule: '9-17',
      }),
      call('limits.set', {
        targetType: 'website',
        target: 'x.com',
        dailyMs: MIN,
        action: 'notify',
      }),
      call('limits.delete', { id: 'one' }),
      call('settings.set', { idleThresholdMinutes: 0 }),
      call('settings.set', { idleThresholdMinutes: 61 }),
      call('settings.set', { downtimeStart: '25:00' }),
      call('settings.set', { noSuchSetting: true }),
      call('tracker.pause', { minutes: 0 }),
      call('tracker.pause', { minutes: -5 }),
      call('focus.start', { minutes: 0 }),
      call('focus.start', { minutes: 24 * 60 + 1 }),
      call('data.export', { format: 'xml' }),
      call('data.wipe'),
      call('apps.setCategory', { appId: 'code' }),
      // Methods without params reject any params, even an empty object.
      call('limits.list', {}),
      // Unknown keys in non-strict params are dropped, not rejected.
      call('usage.web', { from: TODAY, to: NOW, extra: 1 }),
      // Nothing above changed anything.
      call('limits.list'),
      call('settings.get'),
    ],
  ),

  scenario('limits', 'Creating, updating and deleting limits', ({ call }) => [
    call('limits.set', { targetType: 'app', target: 'code', dailyMs: 120 * MIN, action: 'notify' }),
    call('limits.set', {
      targetType: 'domain',
      target: 'reddit.com',
      dailyMs: 15 * MIN,
      action: 'overlay',
      schedule: '22:00-06:00',
    }),
    // Update in place by id.
    call('limits.set', {
      id: 1,
      targetType: 'app',
      target: 'com.spotify.Client',
      dailyMs: 45 * MIN,
      action: 'overlay',
    }),
    // A second limit for the same target replaces the first and keeps its id.
    call('limits.set', {
      targetType: 'domain',
      target: 'youtube.com',
      dailyMs: MIN,
      action: 'notify',
    }),
    call('limits.delete', { id: 2 }),
    call('limits.delete', { id: 999 }),
    call('limits.list'),
  ]),

  scenario('settings', 'Partial updates return the full settings', ({ call }) => [
    call('settings.set', { captureTitles: true }),
    call('settings.set', {
      breakRemindersEnabled: true,
      breakEveryMinutes: 25,
      downtimeStart: '23:15',
      downtimeEnd: '06:45',
    }),
    call('settings.set', {}),
    call('settings.get'),
  ]),

  scenario('apps', 'Changing app categories', ({ call }) => [
    call('apps.setCategory', { appId: 'code', categoryId: 2 }),
    call('apps.setCategory', { appId: 'org.gnome.Nautilus', categoryId: null }),
    call('apps.setCategory', { appId: 'com.example.Missing', categoryId: 1 }),
    call('apps.list'),
    call('usage.summary', { from: TODAY, to: NOW, groupBy: 'category' }),
  ]),

  scenario('tracker', 'Pausing and resuming tracking', ({ call }) => [
    call('tracker.pause', { minutes: 15 }),
    call('tracker.status'),
    call('tracker.pause', { minutes: 60 }),
    call('tracker.resume'),
    call('tracker.status'),
    call('tracker.resume'),
  ]),

  scenario('focus', 'Focus mode', ({ call }) => [
    call('focus.start', { minutes: 25 }),
    call('tracker.status'),
    call('focus.start', { minutes: 24 * 60 }),
    call('focus.stop'),
    call('tracker.status'),
    call('focus.stop'),
  ]),

  scenario('export', 'CSV and JSON export', ({ call }) => [
    call('data.export', { format: 'csv' }),
    call('data.export', { format: 'json' }),
    call('data.export', { format: 'json', from: TODAY, to: NOW }),
    call('data.export', { format: 'csv', from: NOW, to: NOW }),
  ]),

  scenario('wipe', 'Deleting history, then everything', ({ call }) => [
    call('data.wipe', {}),
    call('usage.summary', { from: TODAY - 6 * DAY, to: NOW, groupBy: 'app' }),
    call('usage.web', { from: TODAY - 6 * DAY, to: NOW }),
    call('limits.list'),
    call('settings.get'),
    call('apps.list'),
    call('data.wipe', { everything: true }),
    call('limits.list'),
    call('settings.get'),
    call('apps.list'),
    call('categories.list'),
  ]),

  scenario('browser', 'Messages from the browser native host get no reply', ({ call, notify }) => [
    notify('browser.activeTab', { domain: 'example.com', active: true }),
    notify('browser.activeTab', { active: false }),
    notify('browser.activeTab', { domain: 'x'.repeat(300), active: true }),
    notify('browser.activeTab', { domain: 'example.com' }),
    // With no app in focus, nothing is recorded.
    call('usage.web', { from: TODAY, to: NOW }),
  ]),
];
