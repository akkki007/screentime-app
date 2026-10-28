/**
 * The optional daily planner: types for its RPC surface and the pure logic
 * around it. Nothing here is imported by the main bundle; the planner views are
 * loaded with a dynamic import() only when the setting is on, so a user who
 * never enables it downloads and runs none of this.
 */
import type { Category, Limit, Settings } from '@screentime/shared';
import { getBridge } from './api';

export type PlannerProvider = 'local' | 'remote';

/** The planner's settings keys, stored through the ordinary settings.get/set. */
export type PlannerSettings = {
  plannerEnabled: boolean;
  plannerProvider: PlannerProvider;
  plannerEndpoint: string;
  plannerModel: string;
  plannerServerPath: string;
  plannerModelPath: string;
  plannerIdleMinutes: number;
};

export const PLANNER_DEFAULTS: PlannerSettings = {
  plannerEnabled: false,
  plannerProvider: 'local',
  plannerEndpoint: '',
  plannerModel: '',
  plannerServerPath: '',
  plannerModelPath: '',
  plannerIdleMinutes: 5,
};

export const LIMITS = {
  endpoint: 200,
  model: 100,
  serverPath: 300,
  modelPath: 300,
  goal: 500,
  idleMin: 1,
  idleMax: 60,
} as const;

/** Settings as the daemon returns them: the shared schema plus the planner keys. */
export function plannerSettings(settings: Settings | undefined): PlannerSettings {
  return { ...PLANNER_DEFAULTS, ...(settings as Partial<PlannerSettings> | undefined) };
}

export type PlannerState = 'off' | 'starting' | 'ready' | 'error';

export type PlannerStatus = {
  enabled: boolean;
  provider: PlannerProvider;
  state: PlannerState;
  serverFound: boolean;
  modelFound: boolean;
  keySet: boolean;
  /** Hostname the remote provider would contact; empty for the local one. */
  host: string;
  lastError: string | null;
};

export type SuggestionKind = 'limit' | 'focus' | 'break' | 'note';

export type Suggestion = {
  kind: SuggestionKind;
  appId?: string;
  categoryId?: number;
  minutes?: number;
  reason: string;
};

export type Plan = { summary: string; suggestions: Suggestion[] };

export type CategoryGuess = { appId: string; categoryId: number; confidence: number };

/**
 * `planner.*` are not in the shared RPC table (the daemon owns that contract),
 * so these go through the bridge directly with the types above.
 */
export const planner = {
  status: () => getBridge().call('planner.status') as Promise<PlannerStatus>,
  setKey: (key: string) => getBridge().call('planner.setKey', { key }) as Promise<{ ok: true }>,
  suggest: (goal?: string) =>
    getBridge().call('planner.suggest', goal ? { goal } : {}) as Promise<Plan>,
  categorize: (appIds?: string[]) =>
    getBridge().call('planner.categorize', appIds ? { appIds } : {}) as Promise<CategoryGuess[]>,
  stop: () => getBridge().call('planner.stop') as Promise<{ ok: true }>,
  /** Writes planner settings; the shared Settings type doesn't list them. */
  async setSettings(patch: Partial<PlannerSettings>): Promise<Settings> {
    return (await getBridge().call('settings.set', patch)) as Settings;
  },
};

// --- validation ------------------------------------------------------------

const LOOPBACK = new Set(['localhost', '127.0.0.1', '[::1]', '::1']);

/** `undefined` when the endpoint is fine, otherwise what to tell the user. */
export function endpointError(raw: string): string | undefined {
  const value = raw.trim();
  if (!value) return 'Enter the base URL of your provider.';
  if (value.length > LIMITS.endpoint) return `Use at most ${LIMITS.endpoint} characters.`;
  let url: URL;
  try {
    url = new URL(value);
  } catch {
    return 'That is not a URL. Try something like https://api.example.com/v1.';
  }
  if (url.protocol !== 'https:' && url.protocol !== 'http:') return 'Use an https:// address.';
  if (url.username || url.password) {
    return 'Leave credentials out of the URL; use the API key field instead.';
  }
  if (url.protocol === 'http:' && !LOOPBACK.has(url.hostname)) {
    return 'Use https:// so your key and data are encrypted. Plain http is only allowed for this computer.';
  }
  return undefined;
}

/** The hostname a base URL points at, for the disclosure text; '' if it isn't a URL. */
export function hostOf(raw: string): string {
  try {
    return new URL(raw.trim()).host;
  } catch {
    return '';
  }
}

/** Host to show in the disclosure: the daemon's answer, else what the saved URL implies. */
export function disclosureHost(status: Pick<PlannerStatus, 'host'> | undefined, endpoint: string) {
  return status?.host || hostOf(endpoint);
}

export function pathError(raw: string, max: number, label: string): string | undefined {
  if (raw.length > max) return `${label} can be at most ${max} characters.`;
  if (raw.includes('\n')) return `${label} must be on one line.`;
  return undefined;
}

export function idleMinutesError(value: number): string | undefined {
  return Number.isInteger(value) && value >= LIMITS.idleMin && value <= LIMITS.idleMax
    ? undefined
    : `Enter a whole number from ${LIMITS.idleMin} to ${LIMITS.idleMax}.`;
}

/** Why the planner can't be used yet (a setup problem, not a transient error), or undefined. */
export function notReadyReason(status: PlannerStatus, endpoint: string): string | undefined {
  if (!status.enabled) return 'The planner is off.';
  if (status.provider === 'remote') {
    if (endpointError(endpoint)) return 'Add a valid endpoint URL.';
    if (!status.keySet) return 'Save your API key.';
  } else {
    if (!status.serverFound) return 'llama-server was not found.';
    if (!status.modelFound) return 'Choose a .gguf model file.';
  }
  return undefined;
}

/**
 * The Plan card and the categorize action only appear for a planner that is on
 * and not "off". (A local server that idled out may also report "off"; if the
 * daemon turns out to do that, relax this to `status.enabled`.)
 */
export function planCardVisible(status: Pick<PlannerStatus, 'enabled' | 'state'> | undefined) {
  return Boolean(status?.enabled && status.state !== 'off');
}

// --- suggestions -> existing APIs ------------------------------------------

export const FOCUS_DEFAULT_MINUTES = 25;
const DAY_MINUTES = 24 * 60;

function wholeMinutes(minutes: number | undefined): number | undefined {
  if (minutes === undefined || !Number.isFinite(minutes)) return undefined;
  const m = Math.round(minutes);
  return m >= 1 && m <= DAY_MINUTES ? m : undefined;
}

/** Whether Accept does something; break and note rows can only be dismissed. */
export function isActionable(s: Suggestion): boolean {
  return s.kind === 'limit' || s.kind === 'focus';
}

/**
 * The exact `limits.set` params for a limit suggestion, as LimitsView builds
 * them: apps are targeted by appId, categories by name. A limit that already
 * exists for the target is updated (its id is passed) rather than duplicated.
 * Always the gentle "notify" action. Undefined if the suggestion can't be
 * mapped (unknown category, no target, minutes missing or out of range).
 */
export function suggestionToLimit(
  s: Suggestion,
  categories: Pick<Category, 'id' | 'name'>[],
  existing: Pick<Limit, 'id' | 'targetType' | 'target'>[] = [],
): Limit | undefined {
  if (s.kind !== 'limit') return undefined;
  const minutes = wholeMinutes(s.minutes);
  if (minutes === undefined) return undefined;

  let targetType: Limit['targetType'];
  let target: string | undefined;
  if (s.appId) {
    targetType = 'app';
    target = s.appId;
  } else if (s.categoryId !== undefined) {
    targetType = 'category';
    target = categories.find((c) => c.id === s.categoryId)?.name;
  } else {
    return undefined;
  }
  if (!target) return undefined;

  const prior = existing.find((l) => l.targetType === targetType && l.target === target);
  return {
    ...(prior?.id !== undefined ? { id: prior.id } : {}),
    targetType,
    target,
    dailyMs: minutes * 60_000,
    action: 'notify',
  };
}

/** Minutes for `focus.start`: the suggestion's, clamped to what the daemon accepts. */
export function suggestionFocusMinutes(s: Suggestion): number {
  return wholeMinutes(s.minutes) ?? FOCUS_DEFAULT_MINUTES;
}

/** A one-line label for what accepting would do, e.g. "Limit Slack to 45m". */
export function describeAction(
  s: Suggestion,
  label: { app: (appId: string) => string; category: (id: number) => string },
): string {
  const m = wholeMinutes(s.minutes);
  const dur =
    m === undefined
      ? ''
      : m % 60 === 0
        ? `${m / 60}h`
        : m > 60
          ? `${Math.floor(m / 60)}h ${m % 60}m`
          : `${m}m`;
  switch (s.kind) {
    case 'limit': {
      const who = s.appId
        ? label.app(s.appId)
        : s.categoryId !== undefined
          ? label.category(s.categoryId)
          : 'something';
      return dur ? `Limit ${who} to ${dur} a day` : `Limit ${who}`;
    }
    case 'focus':
      return `Start a ${suggestionFocusMinutes(s)} minute focus session`;
    case 'break':
      return dur ? `Take a ${dur} break` : 'Take a break';
    default:
      return 'Note';
  }
}

export function elapsedLabel(seconds: number): string {
  const s = Math.max(0, Math.floor(seconds));
  return s < 60 ? `${s}s` : `${Math.floor(s / 60)}m ${String(s % 60).padStart(2, '0')}s`;
}
