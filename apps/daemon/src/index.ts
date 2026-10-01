import { openDb } from '@screentime/db';
import type { UsageSummaryRequest, UsageSummaryRow } from '@screentime/shared';
import { selectFocusProvider } from './focus-providers';
import type { MethodHandlers } from './rpc-server';
import { startRpcServer } from './rpc-server';
import { Tracker } from './tracker';

async function main() {
  const db = openDb();

  const focusProvider = await selectFocusProvider();
  console.log(`[daemon] using focus provider: ${focusProvider.id}`);

  const debug = process.env.SCREENTIME_DEBUG === '1';
  const tracker = new Tracker(db, focusProvider, {
    log: debug ? (message) => console.log(`[tracker] ${message}`) : undefined,
  });
  tracker.start();

  const handlers: MethodHandlers = {
    'usage.summary': (params) => usageSummary(db, params as UsageSummaryRequest),
    'usage.timeline': () => [],
    'limits.list': () => db.query('SELECT * FROM limits').all(),
    'settings.get': () => ({}),
    'tracker.pause': (params) => {
      const { minutes } = params as { minutes: number };
      tracker.stop();
      const resumeAt = Date.now() + minutes * 60_000;
      setTimeout(() => tracker.start(), minutes * 60_000);
      return { resumeAt };
    },
    'browser.activeTab': ({ domain, active }) => {
      // TODO(Phase 4): merge into `web_sessions` with the same merge rule as `sessions`.
      console.log('[daemon] browser.activeTab ->', { domain, active });
    },
  };

  startRpcServer(db, handlers);
  console.log('[daemon] listening for JSON-RPC over Unix socket');

  const shutdown = () => {
    console.log('[daemon] shutting down');
    tracker.stop();
    db.close();
    process.exit(0);
  };
  process.on('SIGINT', shutdown);
  process.on('SIGTERM', shutdown);
}

function usageSummary(
  db: ReturnType<typeof openDb>,
  { from, to, groupBy }: UsageSummaryRequest,
): UsageSummaryRow[] {
  const column =
    groupBy === 'app' ? 'apps.app_id' : groupBy === 'category' ? 'categories.name' : null;

  if (groupBy === 'hour') {
    return db
      .query(
        `SELECT strftime('%H', start_ts / 1000, 'unixepoch', 'localtime') AS key,
                SUM(end_ts - start_ts) AS ms
         FROM sessions
         WHERE start_ts >= ? AND start_ts < ?
         GROUP BY key`,
      )
      .all(from, to) as UsageSummaryRow[];
  }

  return db
    .query(
      `SELECT ${column} AS key, SUM(sessions.end_ts - sessions.start_ts) AS ms
       FROM sessions
       JOIN apps ON apps.id = sessions.app_id
       LEFT JOIN categories ON categories.id = apps.category_id
       WHERE sessions.start_ts >= ? AND sessions.start_ts < ?
       GROUP BY key`,
    )
    .all(from, to) as UsageSummaryRow[];
}

main().catch((err) => {
  console.error('[daemon] fatal error:', err);
  process.exit(1);
});
