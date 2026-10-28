import { describe, expect, test } from 'bun:test';
import {
  type Suggestion,
  describeAction,
  disclosureHost,
  elapsedLabel,
  endpointError,
  hostOf,
  idleMinutesError,
  isActionable,
  notReadyReason,
  planCardVisible,
  plannerSettings,
  suggestionFocusMinutes,
  suggestionToLimit,
} from './planner';

const categories = [
  { id: 1, name: 'Development' },
  { id: 5, name: 'Entertainment' },
];
const status = {
  enabled: true,
  provider: 'remote' as const,
  state: 'ready' as const,
  serverFound: false,
  modelFound: false,
  keySet: true,
  host: 'api.example.com',
  lastError: null,
};

describe('endpointError', () => {
  test('accepts https URLs with a path', () => {
    expect(endpointError('https://api.openai.com/v1')).toBeUndefined();
    expect(endpointError('  https://example.com:8443/v1  ')).toBeUndefined();
  });
  test('allows plain http only for this computer', () => {
    expect(endpointError('http://localhost:11434/v1')).toBeUndefined();
    expect(endpointError('http://127.0.0.1:8080')).toBeUndefined();
    expect(endpointError('http://[::1]:8080')).toBeUndefined();
    expect(endpointError('http://example.com/v1')).toContain('https://');
  });
  test('rejects empty, malformed, other schemes, credentials and long values', () => {
    expect(endpointError('')).toBeDefined();
    expect(endpointError('not a url')).toBeDefined();
    expect(endpointError('ftp://example.com')).toBeDefined();
    expect(endpointError('https://user:pw@example.com')).toContain('API key');
    expect(endpointError(`https://example.com/${'a'.repeat(200)}`)).toContain('200');
  });
});

describe('host display', () => {
  test('hostOf keeps the port and ignores junk', () => {
    expect(hostOf('https://api.example.com/v1')).toBe('api.example.com');
    expect(hostOf('https://example.com:8443/x')).toBe('example.com:8443');
    expect(hostOf('nope')).toBe('');
  });
  test('the daemon-reported host wins over the typed URL', () => {
    expect(disclosureHost({ host: 'a.example' }, 'https://b.example')).toBe('a.example');
    expect(disclosureHost({ host: '' }, 'https://b.example/v1')).toBe('b.example');
    expect(disclosureHost(undefined, '')).toBe('');
  });
});

describe('suggestionToLimit', () => {
  const app: Suggestion = { kind: 'limit', appId: 'com.slack.Slack', minutes: 45, reason: 'x' };
  test('maps an app limit to limits.set params', () => {
    expect(suggestionToLimit(app, categories)).toEqual({
      targetType: 'app',
      target: 'com.slack.Slack',
      dailyMs: 45 * 60_000,
      action: 'notify',
    });
  });
  test('targets categories by name', () => {
    const s: Suggestion = { kind: 'limit', categoryId: 5, minutes: 60, reason: 'x' };
    expect(suggestionToLimit(s, categories)).toMatchObject({
      targetType: 'category',
      target: 'Entertainment',
      dailyMs: 3_600_000,
    });
  });
  test('updates an existing limit instead of duplicating it', () => {
    const existing = [{ id: 9, targetType: 'app' as const, target: 'com.slack.Slack' }];
    expect(suggestionToLimit(app, categories, existing)?.id).toBe(9);
    const other = [{ id: 9, targetType: 'category' as const, target: 'com.slack.Slack' }];
    expect(suggestionToLimit(app, categories, other)?.id).toBeUndefined();
  });
  test('refuses what it cannot map', () => {
    expect(suggestionToLimit({ ...app, minutes: undefined }, categories)).toBeUndefined();
    expect(suggestionToLimit({ ...app, minutes: 0 }, categories)).toBeUndefined();
    expect(suggestionToLimit({ ...app, minutes: 24 * 60 + 1 }, categories)).toBeUndefined();
    expect(suggestionToLimit({ ...app, minutes: Number.NaN }, categories)).toBeUndefined();
    expect(
      suggestionToLimit({ kind: 'limit', minutes: 30, reason: '' }, categories),
    ).toBeUndefined();
    expect(
      suggestionToLimit({ kind: 'limit', categoryId: 99, minutes: 30, reason: '' }, categories),
    ).toBeUndefined();
    expect(
      suggestionToLimit({ kind: 'focus', minutes: 30, reason: '' }, categories),
    ).toBeUndefined();
  });
  test('rounds fractional minutes to whole ones', () => {
    expect(suggestionToLimit({ ...app, minutes: 44.6 }, categories)?.dailyMs).toBe(45 * 60_000);
  });
});

describe('focus, actions and labels', () => {
  test('focus minutes default to 25 and stay within a day', () => {
    expect(suggestionFocusMinutes({ kind: 'focus', reason: '' })).toBe(25);
    expect(suggestionFocusMinutes({ kind: 'focus', minutes: 50, reason: '' })).toBe(50);
    expect(suggestionFocusMinutes({ kind: 'focus', minutes: 99999, reason: '' })).toBe(25);
  });
  test('only limit and focus rows can be accepted', () => {
    expect(isActionable({ kind: 'limit', reason: '' })).toBe(true);
    expect(isActionable({ kind: 'focus', reason: '' })).toBe(true);
    expect(isActionable({ kind: 'break', reason: '' })).toBe(false);
    expect(isActionable({ kind: 'note', reason: '' })).toBe(false);
  });
  const label = { app: (id: string) => id.toUpperCase(), category: (id: number) => `cat${id}` };
  test('describeAction', () => {
    expect(describeAction({ kind: 'limit', appId: 'a', minutes: 45, reason: '' }, label)).toBe(
      'Limit A to 45m a day',
    );
    expect(describeAction({ kind: 'limit', categoryId: 2, minutes: 120, reason: '' }, label)).toBe(
      'Limit cat2 to 2h a day',
    );
    expect(describeAction({ kind: 'limit', appId: 'a', minutes: 90, reason: '' }, label)).toBe(
      'Limit A to 1h 30m a day',
    );
    expect(describeAction({ kind: 'focus', reason: '' }, label)).toBe(
      'Start a 25 minute focus session',
    );
    expect(describeAction({ kind: 'break', minutes: 10, reason: '' }, label)).toBe(
      'Take a 10m break',
    );
  });
  test('elapsedLabel', () => {
    expect(elapsedLabel(0)).toBe('0s');
    expect(elapsedLabel(59.9)).toBe('59s');
    expect(elapsedLabel(125)).toBe('2m 05s');
  });
});

describe('settings and readiness', () => {
  test('plannerSettings fills defaults', () => {
    expect(plannerSettings(undefined).plannerIdleMinutes).toBe(5);
    expect(plannerSettings({ plannerEnabled: true } as never).plannerProvider).toBe('local');
  });
  test('idle minutes must be 1..60 integers', () => {
    expect(idleMinutesError(5)).toBeUndefined();
    expect(idleMinutesError(0)).toBeDefined();
    expect(idleMinutesError(61)).toBeDefined();
    expect(idleMinutesError(2.5)).toBeDefined();
  });
  test('remote needs a valid endpoint and a saved key', () => {
    expect(notReadyReason(status, 'https://api.example.com/v1')).toBeUndefined();
    expect(notReadyReason(status, 'nope')).toContain('endpoint');
    expect(notReadyReason({ ...status, keySet: false }, 'https://api.example.com')).toContain(
      'key',
    );
  });
  test('local needs the server and a model', () => {
    const local = { ...status, provider: 'local' as const, host: '' };
    expect(notReadyReason(local, '')).toContain('llama-server');
    expect(notReadyReason({ ...local, serverFound: true }, '')).toContain('.gguf');
    expect(notReadyReason({ ...local, serverFound: true, modelFound: true }, '')).toBeUndefined();
    // A transient error doesn't block a retry.
    expect(
      notReadyReason(
        { ...local, serverFound: true, modelFound: true, state: 'error', lastError: 'boom' },
        '',
      ),
    ).toBeUndefined();
  });
  test('the plan card shows whenever the planner is enabled', () => {
    expect(planCardVisible(status)).toBe(true);
    expect(planCardVisible({ ...status, state: 'starting' })).toBe(true);
    // An idled-out local model is "off" but starts again on the next request.
    expect(planCardVisible({ ...status, state: 'off' })).toBe(true);
    expect(planCardVisible({ ...status, enabled: false })).toBe(false);
    expect(planCardVisible(undefined)).toBe(false);
  });
});
