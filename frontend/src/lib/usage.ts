import type { Session, UsageSummaryRow } from '@screentime/shared';
import { daemon } from './api';
import { addDays, dateKey } from './format';

export type RangeUsage = {
  byApp: UsageSummaryRow[];
  byCategory: UsageSummaryRow[];
  web: UsageSummaryRow[];
  /** Hour-of-day buckets ("00".."23"). */
  byHour: UsageSummaryRow[];
  /** Local-date buckets ("YYYY-MM-DD"). */
  byDay: UsageSummaryRow[];
  total: number;
};

export const sum = (rows: UsageSummaryRow[]): number => rows.reduce((n, r) => n + r.ms, 0);

/** Everything the dashboards need for [from, to), fetched in parallel. */
export async function loadRange(from: number, to: number): Promise<RangeUsage> {
  const [byApp, byCategory, web, byHour, byDay] = await Promise.all([
    daemon('usage.summary', { from, to, groupBy: 'app' }),
    daemon('usage.summary', { from, to, groupBy: 'category' }),
    daemon('usage.web', { from, to }),
    daemon('usage.summary', { from, to, groupBy: 'hour' }),
    daemon('usage.summary', { from, to, groupBy: 'day' }),
  ]);
  return { byApp, byCategory, web, byHour, byDay, total: sum(byApp) };
}

export async function loadTotal(from: number, to: number): Promise<number> {
  return sum(await daemon('usage.summary', { from, to, groupBy: 'app' }));
}

export function loadTimeline(date: string): Promise<Session[]> {
  return daemon('usage.timeline', { date });
}

/** One entry per local day in [start, start + days), zero-filled where nothing was recorded. */
export function fillDays(
  start: number,
  days: number,
  rows: UsageSummaryRow[],
): { key: string; ms: number }[] {
  const byKey = new Map(rows.map((r) => [r.key, r.ms]));
  return Array.from({ length: days }, (_, i) => {
    const key = dateKey(addDays(start, i));
    return { key, ms: byKey.get(key) ?? 0 };
  });
}

/** 24 hour-of-day entries, zero-filled. */
export function fillHours(rows: UsageSummaryRow[]): number[] {
  const byKey = new Map(rows.map((r) => [r.key, r.ms]));
  return Array.from({ length: 24 }, (_, h) => byKey.get(String(h).padStart(2, '0')) ?? 0);
}

/**
 * Guards against out-of-order async results: only the latest `next()` ticket
 * is current, so a slow earlier response can't overwrite a newer one.
 */
export function latestOnly() {
  let ticket = 0;
  return {
    next: () => ++ticket,
    isCurrent: (t: number) => t === ticket,
  };
}
