import { openDb } from '@screentime/db';
import { defaultCategoryFor } from '@screentime/shared';
import type { Settings, TrackerStatus } from '@screentime/shared';
import { backfillApps } from './apps';
import { selectFocusProvider } from './focus-providers';
import { DbusNotifier } from './notifier';
import {
  deleteLimit,
  exportData,
  listApps,
  listCategories,
  listLimits,
  setAppCategory,
  setLimit,
  timeline,
  usageSummary,
  usageWeb,
  wipeData,
} from './queries';
import { type RpcServer, isSocketLive, startRpcServer } from './rpc-server';
import { RulesEngine } from './rules';
import { getSettings, patchSettings } from './settings-store';
import { HEARTBEAT_MS, Tracker } from './tracker';
import { WebTracker } from './web-tracker';

async function main() {
  if (await isSocketLive()) {
    console.error('[daemon] another screentime daemon is already running; exiting');
    process.exit(1);
  }

  const db = openDb();
  backfillApps(db);

  let settings: Settings = getSettings(db);
  const debug = process.env.SCREENTIME_DEBUG === '1';

  const focusProvider = await selectFocusProvider();
  console.log(`[daemon] using focus provider: ${focusProvider.id}`);

  const tracker = new Tracker(db, focusProvider, {
    captureTitles: settings.captureTitles,
    idleThresholdMs: settings.idleThresholdMinutes * 60_000,
    log: debug ? (message) => console.log(`[tracker] ${message}`) : undefined,
  });
  const web = new WebTracker(db);

  // Declared up front: the rules engine and tracker events publish through it.
  const hub: { rpc?: RpcServer } = {};

  const rules = new RulesEngine({
    db,
    notifier: new DbusNotifier(),
    now: Date.now,
    settings: () => settings,
    state: () => tracker.state(),
    activeDomain: () => web.activeDomain,
    emitLimitHit: (event) => hub.rpc?.broadcast('event.limitHit', event),
    emitReminder: (event) => hub.rpc?.broadcast('event.reminder', event),
  });

  const status = (): TrackerStatus => {
    const s = tracker.state();
    return {
      paused: s.paused,
      resumeAt: s.resumeAt,
      idle: s.idle,
      currentAppId: s.currentAppId,
      since: s.since,
      focusMode: rules.focusMode(),
    };
  };

  /** Whether a browser is the app currently being counted. */
  const browserInUse = () => {
    const appId = tracker.state().currentAppId;
    return appId !== null && defaultCategoryFor(appId) === 'Browsing';
  };

  tracker.onEvent((event) => {
    rules.onTrackerEvent(event);

    if (event.type === 'idle') web.closeAt(event.at);
    else if (event.type === 'paused') web.closeAt(Date.now());
    else web.sync(browserInUse());

    if (event.type === 'focus')
      hub.rpc?.broadcast('event.focus', { appId: event.appId, since: event.since });
    hub.rpc?.broadcast('event.status', status());
  });

  tracker.start();

  hub.rpc = startRpcServer(db, {
    'usage.summary': (params) => usageSummary(db, params),
    'usage.timeline': ({ date }) => timeline(db, date),
    'usage.web': ({ from, to }) => usageWeb(db, from, to),

    'limits.list': () => listLimits(db),
    'limits.set': (limit) => setLimit(db, limit),
    'limits.delete': ({ id }) => ({ ok: deleteLimit(db, id) }),

    'apps.list': () => listApps(db),
    'apps.setCategory': ({ appId, categoryId }) => ({ ok: setAppCategory(db, appId, categoryId) }),
    'categories.list': () => listCategories(db),

    'settings.get': () => settings,
    'settings.set': (patch) => {
      settings = patchSettings(db, patch);
      tracker.configure({
        captureTitles: settings.captureTitles,
        idleThresholdMs: settings.idleThresholdMinutes * 60_000,
      });
      return settings;
    },

    'tracker.pause': ({ minutes }) => ({ resumeAt: tracker.pause(minutes) }),
    'tracker.resume': () => {
      tracker.resume();
      return { ok: true };
    },
    'tracker.status': () => status(),

    'focus.start': ({ minutes }) => {
      const until = rules.startFocusMode(minutes);
      hub.rpc?.broadcast('event.status', status());
      return { until };
    },
    'focus.stop': () => {
      rules.stopFocusMode();
      hub.rpc?.broadcast('event.status', status());
      return { ok: true };
    },

    'data.export': ({ format, from, to }) => exportData(db, format, from, to),
    'data.wipe': ({ everything }) => {
      wipeData(db, everything);
      // The tracker's open session pointed at a row that no longer exists.
      tracker.restartSession();
      web.reset();
      if (everything) {
        settings = getSettings(db);
        tracker.configure({
          captureTitles: settings.captureTitles,
          idleThresholdMs: settings.idleThresholdMinutes * 60_000,
        });
      }
      return { ok: true };
    },

    'browser.activeTab': (tab) => {
      if (debug) console.log('[daemon] browser.activeTab ->', tab);
      web.handle(tab, browserInUse());
    },
  });
  console.log('[daemon] listening for JSON-RPC over Unix socket');

  const heartbeat = setInterval(() => {
    web.sync(browserInUse());
    rules.tick();
  }, HEARTBEAT_MS);

  const shutdown = () => {
    console.log('[daemon] shutting down');
    clearInterval(heartbeat);
    tracker.stop();
    web.closeAt(Date.now());
    hub.rpc?.stop();
    db.close();
    process.exit(0);
  };
  process.on('SIGINT', shutdown);
  process.on('SIGTERM', shutdown);
}

main().catch((err) => {
  console.error('[daemon] fatal error:', err);
  process.exit(1);
});
