import type { Database } from 'bun:sqlite';
import type {
  AppInfo,
  Category,
  DataExportResponse,
  Limit,
  Session,
  UsageSummaryRequest,
  UsageSummaryRow,
} from '@screentime/shared';
import { addLocalDays, localDateKey, parseLocalDate } from './time';

type SessionRow = {
  id: number;
  app_id: string;
  title: string | null;
  start_ts: number;
  end_ts: number;
  source: 'desktop' | 'browser';
};

const UNCATEGORIZED = 'Uncategorized';

/**
 * Sum of overlap between each session and [from, to). Clipping matters:
 * a session that straddles midnight (or a limit's day boundary) must only
 * count the part inside the window.
 */
const CLIPPED_MS = 'MAX(0, MIN(end_ts, :to) - MAX(start_ts, :from))';

export function usageSummary(db: Database, req: UsageSummaryRequest): UsageSummaryRow[] {
  const { from, to, groupBy } = req;

  if (groupBy === 'hour' || groupBy === 'day') {
    return bucketSessions(db, from, to, groupBy);
  }

  const key = groupBy === 'app' ? 'apps.app_id' : `COALESCE(categories.name, '${UNCATEGORIZED}')`;

  return db
    .query(
      `SELECT ${key} AS key, SUM(${CLIPPED_MS}) AS ms
       FROM sessions
       JOIN apps ON apps.id = sessions.app_id
       LEFT JOIN categories ON categories.id = apps.category_id
       WHERE end_ts > :from AND start_ts < :to
       GROUP BY key
       HAVING ms > 0
       ORDER BY ms DESC`,
    )
    .all({ ':from': from, ':to': to }) as UsageSummaryRow[];
}

/**
 * Splits sessions across hour or day boundaries in local time, so a session
 * running 23:50-00:10 counts 10 minutes towards each day.
 * `hour` buckets by hour of day ("00".."23"); `day` by local date.
 */
function bucketSessions(
  db: Database,
  from: number,
  to: number,
  groupBy: 'hour' | 'day',
): UsageSummaryRow[] {
  const sessions = db
    .query('SELECT start_ts, end_ts FROM sessions WHERE end_ts > ? AND start_ts < ?')
    .all(from, to) as Pick<SessionRow, 'start_ts' | 'end_ts'>[];

  const totals = new Map<string, number>();
  const add = (key: string, ms: number) => totals.set(key, (totals.get(key) ?? 0) + ms);

  for (const s of sessions) {
    let cursor = Math.max(s.start_ts, from);
    const end = Math.min(s.end_ts, to);

    while (cursor < end) {
      const d = new Date(cursor);
      const next =
        groupBy === 'day'
          ? addLocalDays(cursor, 1)
          : new Date(d.getFullYear(), d.getMonth(), d.getDate(), d.getHours() + 1).getTime();
      const sliceEnd = Math.min(end, next);
      const key = groupBy === 'day' ? localDateKey(cursor) : String(d.getHours()).padStart(2, '0');
      add(key, sliceEnd - cursor);
      cursor = sliceEnd;
    }
  }

  return [...totals.entries()]
    .map(([key, ms]) => ({ key, ms }))
    .filter((row) => row.ms > 0)
    .sort((a, b) => a.key.localeCompare(b.key));
}

export function usageWeb(db: Database, from: number, to: number): UsageSummaryRow[] {
  return db
    .query(
      `SELECT domain AS key, SUM(MAX(0, MIN(end_ts, :to) - MAX(start_ts, :from))) AS ms
       FROM web_sessions
       WHERE end_ts > :from AND start_ts < :to
       GROUP BY domain
       HAVING ms > 0
       ORDER BY ms DESC`,
    )
    .all({ ':from': from, ':to': to }) as UsageSummaryRow[];
}

/** Sessions that overlap the given local date, oldest first. */
export function timeline(db: Database, date: string): Session[] {
  const from = parseLocalDate(date);
  const to = addLocalDays(from, 1);

  const rows = db
    .query(
      `SELECT sessions.id, apps.app_id, sessions.title, sessions.start_ts, sessions.end_ts, sessions.source
       FROM sessions JOIN apps ON apps.id = sessions.app_id
       WHERE end_ts > ? AND start_ts < ?
       ORDER BY start_ts`,
    )
    .all(from, to) as SessionRow[];

  return rows.map((r) => ({
    id: r.id,
    appId: r.app_id,
    ...(r.title ? { title: r.title } : {}),
    startTs: r.start_ts,
    endTs: r.end_ts,
    source: r.source,
  }));
}

/** Total tracked ms for an app, category (by name) or domain within [from, to). */
export function usageFor(
  db: Database,
  targetType: Limit['targetType'],
  target: string,
  from: number,
  to: number,
): number {
  const params = { ':from': from, ':to': to, ':target': target };
  let sql: string;

  if (targetType === 'domain') {
    sql = `SELECT SUM(${CLIPPED_MS}) AS ms FROM web_sessions
           WHERE end_ts > :from AND start_ts < :to AND domain = :target`;
  } else if (targetType === 'app') {
    sql = `SELECT SUM(${CLIPPED_MS}) AS ms FROM sessions
           JOIN apps ON apps.id = sessions.app_id
           WHERE end_ts > :from AND start_ts < :to AND apps.app_id = :target`;
  } else {
    sql = `SELECT SUM(${CLIPPED_MS}) AS ms FROM sessions
           JOIN apps ON apps.id = sessions.app_id
           JOIN categories ON categories.id = apps.category_id
           WHERE end_ts > :from AND start_ts < :to AND categories.name = :target`;
  }

  const row = db.query(sql).get(params) as { ms: number | null } | null;
  return row?.ms ?? 0;
}

export function listApps(db: Database): AppInfo[] {
  const rows = db
    .query('SELECT app_id, name, icon, category_id FROM apps ORDER BY COALESCE(name, app_id)')
    .all() as {
    app_id: string;
    name: string | null;
    icon: string | null;
    category_id: number | null;
  }[];
  return rows.map((r) => ({
    appId: r.app_id,
    name: r.name,
    icon: r.icon,
    categoryId: r.category_id,
  }));
}

/** Returns false when the app or category doesn't exist. */
export function setAppCategory(db: Database, appId: string, categoryId: number | null): boolean {
  if (categoryId !== null) {
    const exists = db.query('SELECT 1 FROM categories WHERE id = ?').get(categoryId);
    if (!exists) return false;
  }
  const result = db
    .query('UPDATE apps SET category_id = ? WHERE app_id = ?')
    .run(categoryId, appId);
  return result.changes > 0;
}

export function listCategories(db: Database): Category[] {
  return db
    .query('SELECT id, name, color, productive FROM categories ORDER BY id')
    .all() as Category[];
}

type LimitRow = {
  id: number;
  target_type: Limit['targetType'];
  target: string;
  daily_ms: number;
  schedule: string | null;
  action: Limit['action'];
};

export function listLimits(db: Database): Limit[] {
  const rows = db.query('SELECT * FROM limits ORDER BY id').all() as LimitRow[];
  return rows.map(limitFromRow);
}

function limitFromRow(r: LimitRow): Limit {
  return {
    id: r.id,
    targetType: r.target_type,
    target: r.target,
    dailyMs: r.daily_ms,
    ...(r.schedule ? { schedule: r.schedule } : {}),
    action: r.action,
  };
}

/** Inserts a limit, or updates it when `id` is given or one already exists for the target. */
export function setLimit(db: Database, limit: Limit): Limit {
  const values = [
    limit.targetType,
    limit.target,
    limit.dailyMs,
    limit.schedule ?? null,
    limit.action,
  ];

  if (limit.id !== undefined) {
    const result = db
      .query(
        'UPDATE limits SET target_type = ?, target = ?, daily_ms = ?, schedule = ?, action = ? WHERE id = ?',
      )
      .run(...values, limit.id);
    if (result.changes === 0) throw new Error(`no limit with id ${limit.id}`);
    return { ...limit, id: limit.id };
  }

  db.query(
    `INSERT INTO limits (target_type, target, daily_ms, schedule, action) VALUES (?, ?, ?, ?, ?)
     ON CONFLICT(target_type, target) DO UPDATE SET
       daily_ms = excluded.daily_ms, schedule = excluded.schedule, action = excluded.action`,
  ).run(...values);

  const row = db
    .query('SELECT * FROM limits WHERE target_type = ? AND target = ?')
    .get(limit.targetType, limit.target) as LimitRow;
  return limitFromRow(row);
}

export function deleteLimit(db: Database, id: number): boolean {
  return db.query('DELETE FROM limits WHERE id = ?').run(id).changes > 0;
}

/** Exports tracked sessions (desktop and web) in [from, to] as CSV or JSON. */
export function exportData(
  db: Database,
  format: 'csv' | 'json',
  from = 0,
  to = Number.MAX_SAFE_INTEGER,
  now = Date.now(),
): DataExportResponse {
  const desktop = db
    .query(
      `SELECT apps.app_id AS target, sessions.start_ts, sessions.end_ts
       FROM sessions JOIN apps ON apps.id = sessions.app_id
       WHERE end_ts > ? AND start_ts < ? ORDER BY start_ts`,
    )
    .all(from, to) as { target: string; start_ts: number; end_ts: number }[];
  const web = db
    .query(
      'SELECT domain AS target, start_ts, end_ts FROM web_sessions WHERE end_ts > ? AND start_ts < ? ORDER BY start_ts',
    )
    .all(from, to) as { target: string; start_ts: number; end_ts: number }[];

  const rows = [
    ...desktop.map((r) => ({ source: 'desktop', ...r })),
    ...web.map((r) => ({ source: 'web', ...r })),
  ].sort((a, b) => a.start_ts - b.start_ts);

  const stamp = localDateKey(now);
  if (format === 'json') {
    const content = JSON.stringify(
      rows.map((r) => ({
        source: r.source,
        target: r.target,
        start: new Date(r.start_ts).toISOString(),
        end: new Date(r.end_ts).toISOString(),
        durationMs: r.end_ts - r.start_ts,
      })),
      null,
      2,
    );
    return { filename: `screentime-${stamp}.json`, content };
  }

  const lines = ['source,target,start,end,duration_ms'];
  for (const r of rows) {
    lines.push(
      [
        r.source,
        csvCell(r.target),
        new Date(r.start_ts).toISOString(),
        new Date(r.end_ts).toISOString(),
        r.end_ts - r.start_ts,
      ].join(','),
    );
  }
  return { filename: `screentime-${stamp}.csv`, content: `${lines.join('\n')}\n` };
}

function csvCell(value: string): string {
  // Quote when needed, and defuse spreadsheet formula injection from app/domain names.
  const safe = /^[=+\-@\t\r]/.test(value) ? `'${value}` : value;
  return /[",\n]/.test(safe) ? `"${safe.replaceAll('"', '""')}"` : safe;
}

/** Deletes tracked usage. With `everything`, also settings, limits and app/category data. */
export function wipeData(db: Database, everything: boolean): void {
  db.transaction(() => {
    db.exec('DELETE FROM sessions; DELETE FROM web_sessions;');
    if (everything) {
      db.exec('DELETE FROM limits; DELETE FROM settings; DELETE FROM apps;');
    }
  })();
  // Reclaim the space so deleted history isn't left readable in free pages.
  db.exec('VACUUM');
  db.exec('PRAGMA wal_checkpoint(TRUNCATE)');
}
