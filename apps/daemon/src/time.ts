/**
 * Local-time helpers. Timestamps are stored as Unix ms in UTC; only the
 * boundaries of "a day" and "an hour" depend on the user's timezone.
 */

export function startOfLocalDay(ts: number): number {
  const d = new Date(ts);
  return new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime();
}

/** Start of the local day `days` after the one containing `ts` (DST-safe, unlike adding 24 h). */
export function addLocalDays(ts: number, days: number): number {
  const d = new Date(ts);
  return new Date(d.getFullYear(), d.getMonth(), d.getDate() + days).getTime();
}

export function localDateKey(ts: number): string {
  const d = new Date(ts);
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
}

/** Parses `YYYY-MM-DD` as local midnight. Throws on anything else. */
export function parseLocalDate(date: string): number {
  const match = /^(\d{4})-(\d{2})-(\d{2})$/.exec(date);
  if (!match) throw new Error(`invalid date: ${date}`);
  const [, y, m, d] = match;
  const ts = new Date(Number(y), Number(m) - 1, Number(d)).getTime();
  if (Number.isNaN(ts)) throw new Error(`invalid date: ${date}`);
  return ts;
}

/** Minutes since local midnight for `ts`. */
export function minutesIntoDay(ts: number): number {
  const d = new Date(ts);
  return d.getHours() * 60 + d.getMinutes();
}

/** Parses `HH:MM` into minutes since midnight. */
export function parseClock(time: string): number {
  const [h, m] = time.split(':').map(Number);
  return (h ?? 0) * 60 + (m ?? 0);
}

/**
 * Whether `minute` falls in [start, end), where the window may wrap past
 * midnight (e.g. 22:00-07:00). An empty window (start == end) is never active.
 */
export function inDailyWindow(minute: number, start: number, end: number): boolean {
  if (start === end) return false;
  return start < end ? minute >= start && minute < end : minute >= start || minute < end;
}
