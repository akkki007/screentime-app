/**
 * An in-memory stand-in for the daemon, used when the UI runs in a plain
 * browser (`bun run hmr`, headless screenshots). It mirrors the real RPC
 * contract closely enough to exercise every screen, with deterministic
 * fixture data so screenshots are reproducible.
 */
import {
  DEFAULT_CATEGORIES,
  DEFAULT_SETTINGS,
  type Limit,
  type Settings,
} from '@screentime/shared';
import type { ConnectionState } from './bridge';
import type { Bridge } from './bridge';
import { addDays, dateKey, startOfDay } from './format';

const MIN = 60_000;
const HOUR = 60 * MIN;

type Row = { key: string; ms: number };
type Sess = { id: number; appId: string; start: number; end: number };

const APPS: { appId: string; name: string; category: string; weight: number }[] = [
  { appId: 'code', name: 'Visual Studio Code', category: 'Development', weight: 30 },
  { appId: 'org.gnome.Ptyxis', name: 'Terminal', category: 'Development', weight: 18 },
  { appId: 'org.mozilla.firefox', name: 'Firefox', category: 'Browsing', weight: 26 },
  { appId: 'com.slack.Slack', name: 'Slack', category: 'Communication', weight: 9 },
  { appId: 'org.telegram.desktop', name: 'Telegram', category: 'Communication', weight: 4 },
  { appId: 'org.gnome.Evince', name: 'Document Viewer', category: 'Productivity', weight: 4 },
  { appId: 'org.gnome.Nautilus', name: 'Files', category: 'Utilities', weight: 3 },
  { appId: 'com.spotify.Client', name: 'Spotify', category: 'Entertainment', weight: 3 },
  { appId: 'com.valvesoftware.Steam', name: 'Steam', category: 'Entertainment', weight: 3 },
];

const DOMAINS = [
  'github.com',
  'stackoverflow.com',
  'youtube.com',
  'news.ycombinator.com',
  'docs.python.org',
  'developer.mozilla.org',
  'reddit.com',
  'wikipedia.org',
];

/** Small seeded PRNG so fixtures are identical on every load. */
function rng(seed: number) {
  let s = seed >>> 0;
  return () => {
    s = (s + 0x6d2b79f5) >>> 0;
    let t = s;
    t = Math.imul(t ^ (t >>> 15), t | 1);
    t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}

function generate(now: number): {
  sessions: Sess[];
  web: { domain: string; start: number; end: number }[];
} {
  const rand = rng(42);
  const sessions: Sess[] = [];
  const web: { domain: string; start: number; end: number }[] = [];
  const total = APPS.reduce((n, a) => n + a.weight, 0);
  let id = 1;

  const pick = (evening: boolean) => {
    let r = rand() * total;
    for (const a of APPS) {
      const w =
        evening && a.category === 'Entertainment'
          ? a.weight * 5
          : evening && a.category === 'Development'
            ? a.weight * 0.3
            : a.weight;
      r -= w;
      if (r <= 0) return a;
    }
    return APPS[0] as (typeof APPS)[number];
  };

  for (let back = 13; back >= 0; back--) {
    const day = addDays(startOfDay(now), -back);
    const weekend = [0, 6].includes(new Date(day).getDay());
    let t = day + (weekend ? 10.5 : 9 + rand() * 0.8) * HOUR;
    const stop = weekend ? day + 17 * HOUR : day + (17.5 + rand() * 2) * HOUR;

    while (t < stop && t < now) {
      // Lunch break
      if (!weekend && t > day + 12.7 * HOUR && t < day + 13.7 * HOUR) {
        t = day + 13.7 * HOUR;
        continue;
      }
      const evening = t > day + 17 * HOUR;
      const app = pick(evening);
      const len = (4 + rand() * 40) * MIN;
      const end = Math.min(t + len, now, stop + 2 * HOUR);
      if (end - t > 30_000) {
        sessions.push({ id: id++, appId: app.appId, start: Math.round(t), end: Math.round(end) });
        if (app.appId === 'org.mozilla.firefox') {
          const domain = DOMAINS[Math.floor(rand() * DOMAINS.length)] ?? 'example.com';
          web.push({ domain, start: Math.round(t), end: Math.round(end) });
        }
      }
      t = end + rand() * 6 * MIN;
    }
  }
  // Make today trip a limit or two so the "over limit" states are visible.
  const today = startOfDay(now);
  const early: [string, number, number][] = [
    ['com.spotify.Client', 7.4, 8.1],
    ['com.valvesoftware.Steam', 8.1, 8.5],
  ];
  for (const [appId, from, to] of early) {
    const start = today + from * HOUR;
    const end = Math.min(today + to * HOUR, now);
    if (end - start > MIN) sessions.push({ id: id++, appId, start, end });
  }
  web.push({
    domain: 'youtube.com',
    start: today + 7.4 * HOUR,
    end: Math.min(today + 7.95 * HOUR, now),
  });
  return { sessions, web };
}

function clipped(start: number, end: number, from: number, to: number): number {
  return Math.max(0, Math.min(end, to) - Math.max(start, from));
}

export function createMockBridge(): Bridge {
  const now = Date.now();
  const data = generate(now);
  let sessions = data.sessions;
  let web = data.web;

  const categories = DEFAULT_CATEGORIES.map((c, i) => ({
    id: i + 1,
    name: c.name,
    color: c.color,
    productive: c.productive,
  }));
  const apps = APPS.map((a) => ({
    appId: a.appId,
    name: a.name,
    icon: null as string | null,
    categoryId: categories.find((c) => c.name === a.category)?.id ?? null,
  }));
  const categoryName = (appId: string) => {
    const cid = apps.find((a) => a.appId === appId)?.categoryId;
    return categories.find((c) => c.id === cid)?.name ?? 'Uncategorized';
  };

  let settings: Settings = { ...DEFAULT_SETTINGS };
  const params = new URLSearchParams(location.search);
  if (params.get('onboarding') !== '1') settings.onboardingDone = true;
  if (params.get('breaks') === '1')
    settings = { ...settings, breakRemindersEnabled: true, downtimeEnabled: true };

  let limits: Limit[] = [
    { id: 1, targetType: 'app', target: 'com.spotify.Client', dailyMs: 30 * MIN, action: 'notify' },
    { id: 2, targetType: 'category', target: 'Entertainment', dailyMs: HOUR, action: 'overlay' },
    {
      id: 3,
      targetType: 'domain',
      target: 'youtube.com',
      dailyMs: 45 * MIN,
      action: 'notify',
      schedule: '09:00-18:00',
    },
  ];
  let nextLimitId = 4;

  const current = { appId: 'org.mozilla.firefox', since: now - 12 * MIN };
  let pausedUntil: number | null = null;
  let focusUntil: number | null = null;

  let eventHandler: Bridge['onEvent'] extends (h: infer H) => void ? H : never = () => {};
  let connectionHandler: (s: ConnectionState) => void = () => {};
  let connected = params.get('disconnected') !== '1';

  const status = () => ({
    paused: pausedUntil !== null,
    resumeAt: pausedUntil,
    idle: false,
    currentAppId: pausedUntil !== null ? null : current.appId,
    since: pausedUntil !== null ? null : current.since,
    focusMode: { active: focusUntil !== null, until: focusUntil },
  });
  const emitStatus = () => eventHandler('event.status', status());

  function summary(from: number, to: number, groupBy: string): Row[] {
    const totals = new Map<string, number>();
    const add = (k: string, ms: number) => ms > 0 && totals.set(k, (totals.get(k) ?? 0) + ms);

    for (const s of sessions) {
      if (s.end <= from || s.start >= to) continue;
      if (groupBy === 'app') add(s.appId, clipped(s.start, s.end, from, to));
      else if (groupBy === 'category')
        add(categoryName(s.appId), clipped(s.start, s.end, from, to));
      else {
        let cursor = Math.max(s.start, from);
        const end = Math.min(s.end, to);
        while (cursor < end) {
          const d = new Date(cursor);
          const next =
            groupBy === 'day'
              ? addDays(cursor, 1)
              : new Date(d.getFullYear(), d.getMonth(), d.getDate(), d.getHours() + 1).getTime();
          const slice = Math.min(end, next);
          add(
            groupBy === 'day' ? dateKey(cursor) : String(d.getHours()).padStart(2, '0'),
            slice - cursor,
          );
          cursor = slice;
        }
      }
    }
    const rows = [...totals.entries()].map(([key, ms]) => ({ key, ms }));
    return groupBy === 'hour' || groupBy === 'day'
      ? rows.sort((a, b) => a.key.localeCompare(b.key))
      : rows.sort((a, b) => b.ms - a.ms);
  }

  // biome-ignore lint/suspicious/noExplicitAny: fixture handlers each take a differently shaped payload
  const handlers: Record<string, (p: any) => unknown> = {
    'tracker.status': () => status(),
    'settings.get': () => settings,
    'settings.set': (patch: Partial<Settings>) => {
      if (
        patch.idleThresholdMinutes !== undefined &&
        (patch.idleThresholdMinutes < 1 || patch.idleThresholdMinutes > 60)
      ) {
        throw new Error('idleThresholdMinutes: Number must be between 1 and 60');
      }
      settings = { ...settings, ...patch };
      return settings;
    },
    'categories.list': () => categories,
    'apps.list': () => apps,
    'apps.setCategory': ({ appId, categoryId }: { appId: string; categoryId: number | null }) => {
      const app = apps.find((a) => a.appId === appId);
      if (!app) return { ok: false };
      app.categoryId = categoryId;
      return { ok: true };
    },
    'usage.summary': ({ from, to, groupBy }: { from: number; to: number; groupBy: string }) =>
      summary(from, to, groupBy),
    'usage.timeline': ({ date }: { date: string }) => {
      const [y, m, d] = date.split('-').map(Number);
      const from = new Date(y ?? 0, (m ?? 1) - 1, d ?? 1).getTime();
      const to = addDays(from, 1);
      return sessions
        .filter((s) => s.end > from && s.start < to)
        .map((s) => ({
          id: s.id,
          appId: s.appId,
          startTs: s.start,
          endTs: s.end,
          source: 'desktop',
        }));
    },
    'usage.web': ({ from, to }: { from: number; to: number }) => {
      const totals = new Map<string, number>();
      for (const w of web)
        totals.set(w.domain, (totals.get(w.domain) ?? 0) + clipped(w.start, w.end, from, to));
      return [...totals.entries()]
        .filter(([, ms]) => ms > 0)
        .map(([key, ms]) => ({ key, ms }))
        .sort((a, b) => b.ms - a.ms);
    },
    'limits.list': () => limits,
    'limits.set': (limit: Limit) => {
      const existing = limits.find(
        (l) =>
          l.id === limit.id ||
          (limit.id === undefined &&
            l.targetType === limit.targetType &&
            l.target === limit.target),
      );
      if (existing) {
        Object.assign(existing, limit, { id: existing.id });
        return existing;
      }
      const created = { ...limit, id: nextLimitId++ };
      limits = [...limits, created];
      return created;
    },
    'limits.delete': ({ id }: { id: number }) => {
      const before = limits.length;
      limits = limits.filter((l) => l.id !== id);
      return { ok: limits.length < before };
    },
    'tracker.pause': ({ minutes }: { minutes: number }) => {
      pausedUntil = Date.now() + minutes * MIN;
      emitStatus();
      return { resumeAt: pausedUntil };
    },
    'tracker.resume': () => {
      pausedUntil = null;
      emitStatus();
      return { ok: true };
    },
    'focus.start': ({ minutes }: { minutes: number }) => {
      focusUntil = Date.now() + minutes * MIN;
      emitStatus();
      return { until: focusUntil };
    },
    'focus.stop': () => {
      focusUntil = null;
      emitStatus();
      return { ok: true };
    },
    'data.export': () => ({ filename: 'screentime-fixture.csv', content: 'source,target\n' }),
    'data.wipe': ({ everything }: { everything: boolean }) => {
      sessions = [];
      web = [];
      if (everything) {
        limits = [];
        settings = { ...DEFAULT_SETTINGS };
      }
      return { ok: true };
    },
  };

  // Handy from the console / headless runs: `__mock.limitHit(2)`, `__mock.disconnect()`.
  (window as unknown as { __mock: unknown }).__mock = {
    limitHit: (limitId = 2, action = 'overlay') =>
      eventHandler('event.limitHit', { limitId, action }),
    reminder: (message = "You've been active for 50 min. Take a 5 minute break.") =>
      eventHandler('event.reminder', { kind: 'break', message }),
    disconnect: () => {
      connected = false;
      connectionHandler({ connected: false });
    },
    connect: () => {
      connected = true;
      connectionHandler({ connected: true });
    },
  };

  return {
    mode: 'mock',
    async call(method, p) {
      if (!connected) throw new Error('The Screentime daemon is not running.');
      const handler = handlers[method];
      if (!handler) throw new Error(`mock: unhandled method ${method}`);
      await new Promise((r) => setTimeout(r, 15)); // feel like a real round trip
      return handler(p);
    },
    saveExport: async ({ format }) => ({ path: `~/Downloads/screentime-fixture.${format}` }),
    openDashboard: async () => console.info('[mock] open dashboard'),
    quitApp: async () => console.info('[mock] quit'),
    closeWindow: async () => console.info('[mock] close window'),
    connection: async () => (connected ? { connected: true } : { connected: false }),
    onEvent: (h) => {
      eventHandler = h;
    },
    onConnection: (h) => {
      connectionHandler = h;
    },
  };
}
