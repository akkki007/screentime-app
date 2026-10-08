/** Formatting helpers for the dashboard. Pure, so they're unit tested. */

const MIN = 60_000;
const HOUR = 60 * MIN;

/** "2 h 5 min", "45 min", "<1 min". Rounds to the minute. */
export function formatDuration(ms: number): string {
  if (ms <= 0) return '0 min';
  const totalMinutes = Math.round(ms / MIN);
  if (totalMinutes < 1) return '<1 min';
  const h = Math.floor(totalMinutes / 60);
  const m = totalMinutes % 60;
  if (h === 0) return `${m} min`;
  return m === 0 ? `${h} h` : `${h} h ${m} min`;
}

/** Compact form for chart axes and tight spaces: "2h 5m". */
export function formatShort(ms: number): string {
  const totalMinutes = Math.round(ms / MIN);
  if (totalMinutes < 1) return ms > 0 ? '<1m' : '0m';
  const h = Math.floor(totalMinutes / 60);
  const m = totalMinutes % 60;
  if (h === 0) return `${m}m`;
  return m === 0 ? `${h}h` : `${h}h ${m}m`;
}

/** Local midnight of the day containing `ts`. */
export function startOfDay(ts: number): number {
  const d = new Date(ts);
  return new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime();
}

/** Local midnight `days` after the day containing `ts` (DST-safe). */
export function addDays(ts: number, days: number): number {
  const d = new Date(ts);
  return new Date(d.getFullYear(), d.getMonth(), d.getDate() + days).getTime();
}

/** `YYYY-MM-DD` in local time, matching the daemon's date keys. */
export function dateKey(ts: number): string {
  const d = new Date(ts);
  const pad = (n: number) => String(n).padStart(2, '0');
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}`;
}

/** "Mon", "Tue", ... for a `YYYY-MM-DD` key. */
export function weekdayShort(key: string): string {
  const [y, m, d] = key.split('-').map(Number);
  return new Date(y ?? 0, (m ?? 1) - 1, d ?? 1).toLocaleDateString(undefined, { weekday: 'short' });
}

/** "Oct 1" for a `YYYY-MM-DD` key. */
export function dayMonth(key: string): string {
  const [y, m, d] = key.split('-').map(Number);
  return new Date(y ?? 0, (m ?? 1) - 1, d ?? 1).toLocaleDateString(undefined, {
    month: 'short',
    day: 'numeric',
  });
}

/** "14:05" in local time. */
export function clockTime(ts: number): string {
  return new Date(ts).toLocaleTimeString(undefined, {
    hour: '2-digit',
    minute: '2-digit',
    hour12: false,
  });
}

/**
 * Display name for an app: its desktop-entry name, else a readable form of
 * the ID ("org.gnome.Ptyxis" -> "Ptyxis", "firefox_firefox" -> "Firefox").
 */
export function appLabel(appId: string, name?: string | null): string {
  if (name) return name;
  const last = appId.split('.').filter(Boolean).at(-1) ?? appId;
  const base = last.split('_')[0] ?? last;
  return base.charAt(0).toUpperCase() + base.slice(1);
}

/** Share of `part` in `total`, as a whole percentage (0 when total is 0). */
export function percent(part: number, total: number): number {
  return total > 0 ? Math.round((part / total) * 100) : 0;
}

/** Parses "1h30", "90", "1:30", "45m" style input into ms; undefined if unparseable. */
export function parseDurationInput(text: string): number | undefined {
  const s = text.trim().toLowerCase();
  if (!s) return undefined;

  const colon = /^(\d+):([0-5]?\d)$/.exec(s);
  if (colon) return (Number(colon[1]) * 60 + Number(colon[2])) * MIN;

  const parts = /^(?:(\d+(?:\.\d+)?)\s*h(?:ours?|rs?)?)?\s*(?:(\d+)\s*m?(?:in(?:utes?)?)?)?$/.exec(
    s,
  );
  if (parts && (parts[1] || parts[2])) {
    const ms = Number(parts[1] ?? 0) * HOUR + Number(parts[2] ?? 0) * MIN;
    return ms > 0 ? Math.round(ms) : undefined;
  }
  return undefined;
}
