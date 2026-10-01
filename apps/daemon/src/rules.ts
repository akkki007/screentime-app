import type { Database } from 'bun:sqlite';
import type { Limit, LimitHitEvent, ReminderEvent, Settings } from '@screentime/shared';
import type { Notifier } from './notifier';
import { listLimits, usageFor } from './queries';
import {
  addLocalDays,
  inDailyWindow,
  localDateKey,
  minutesIntoDay,
  parseClock,
  startOfLocalDay,
} from './time';
import type { TrackerEvent, TrackerState } from './tracker';

/** Overlay/block limits re-appear this often while the user keeps using the app. */
const LIMIT_REPEAT_MS = 5 * 60_000;
const BREAK_REPEAT_MS = 10 * 60_000;
const DOWNTIME_REPEAT_MS = 15 * 60_000;
const FOCUS_NUDGE_REPEAT_MS = 60_000;
/** A gap this long between ticks means the machine was suspended. */
const SUSPEND_GAP_MS = 60_000;

export type RulesDeps = {
  db: Database;
  notifier: Notifier;
  now: () => number;
  settings: () => Settings;
  state: () => TrackerState;
  /** Domain of the browser tab currently being counted, if any. */
  activeDomain: () => string | undefined;
  emitLimitHit: (event: LimitHitEvent) => void;
  emitReminder: (event: ReminderEvent) => void;
};

type LimitProgress = { day: string; lastEmit: number };

/**
 * The wellbeing rules: daily limits, break reminders, downtime and focus mode.
 *
 * Everything is derived from the database and the clock on each `tick()`
 * rather than from timers, so a suspend/resume can't make a limit miss or
 * double-fire: after wake the next tick simply sees the new totals.
 * v1 limits are nudges (notification + UI overlay); real enforcement is the
 * v2 privileged helper.
 */
export class RulesEngine {
  private readonly progress = new Map<number, LimitProgress>();

  // Break reminders
  private activeSince: number;
  private awaySince: number | null = null;
  private lastBreakReminder = 0;
  private lastTick: number;

  private lastDowntimeNotice = 0;

  private focusUntil: number | null = null;
  private lastFocusNudge = 0;

  constructor(private readonly deps: RulesDeps) {
    this.activeSince = deps.now();
    this.lastTick = deps.now();
  }

  focusMode(): { active: boolean; until: number | null } {
    return { active: this.focusUntil !== null, until: this.focusUntil };
  }

  startFocusMode(minutes: number): number {
    const now = this.deps.now();
    this.focusUntil = now + minutes * 60_000;
    this.lastFocusNudge = 0;
    return this.focusUntil;
  }

  stopFocusMode(): void {
    this.focusUntil = null;
  }

  /** Feed tracker events so break accounting knows when the user was away. */
  onTrackerEvent(event: TrackerEvent): void {
    if (event.type === 'idle') this.leave(event.at);
    else if (event.type === 'paused') this.leave(this.deps.now());
    else if (event.type === 'active') this.returnFromAway(event.at);
    else if (event.type === 'resumed') this.returnFromAway(this.deps.now());
    else if (event.type === 'focus') this.checkFocusMode(event.appId);
  }

  /** Call periodically (the daemon uses the tracker's 5 s heartbeat). */
  tick(): void {
    const now = this.deps.now();
    const settings = this.deps.settings();

    // Suspend shows up as a long gap between ticks: count it as time away.
    if (now - this.lastTick > SUSPEND_GAP_MS) {
      this.leave(this.lastTick);
      this.returnFromAway(now);
    }
    this.lastTick = now;

    this.checkLimits(now);
    this.checkBreak(now, settings);
    this.checkDowntime(now, settings);
    this.checkFocusExpiry(now);
  }

  // --- limits -------------------------------------------------------------

  private checkLimits(now: number): void {
    const from = startOfLocalDay(now);
    const to = addLocalDays(now, 1);
    const day = localDateKey(now);

    for (const limit of listLimits(this.deps.db)) {
      if (limit.id === undefined || !withinSchedule(limit.schedule, now)) continue;

      const used = usageFor(this.deps.db, limit.targetType, limit.target, from, to);
      if (used < limit.dailyMs) continue;

      const seen = this.progress.get(limit.id);
      const first = seen === undefined || seen.day !== day;
      const repeat =
        seen !== undefined &&
        !first &&
        limit.action !== 'notify' &&
        this.isInUse(limit) &&
        now - seen.lastEmit >= LIMIT_REPEAT_MS;
      if (!first && !repeat) continue;

      this.progress.set(limit.id, { day, lastEmit: now });
      this.deps.emitLimitHit({ limitId: limit.id, action: limit.action });
      if (first) {
        void this.deps.notifier.notify(
          'Daily limit reached',
          `${this.displayName(limit)}: you've used ${formatDuration(limit.dailyMs)} today.`,
        );
      }
    }
  }

  private isInUse(limit: Limit): boolean {
    const state = this.deps.state();
    if (limit.targetType === 'domain') return this.deps.activeDomain() === limit.target;
    if (!state.currentAppId) return false;
    if (limit.targetType === 'app') return state.currentAppId === limit.target;
    return this.categoryOf(state.currentAppId)?.name === limit.target;
  }

  // --- break reminders ----------------------------------------------------

  private leave(at: number): void {
    this.awaySince ??= at;
  }

  private returnFromAway(at: number): void {
    if (this.awaySince === null) return;
    const awayMs = at - this.awaySince;
    this.awaySince = null;
    if (awayMs >= this.deps.settings().breakLengthMinutes * 60_000) {
      this.activeSince = at;
      this.lastBreakReminder = 0;
    }
  }

  private checkBreak(now: number, settings: Settings): void {
    if (!settings.breakRemindersEnabled || this.awaySince !== null) return;
    const state = this.deps.state();
    if (state.idle || state.paused) return;

    const activeMs = now - this.activeSince;
    if (activeMs < settings.breakEveryMinutes * 60_000) return;
    if (now - this.lastBreakReminder < BREAK_REPEAT_MS) return;

    this.lastBreakReminder = now;
    const message = `You've been active for ${formatDuration(activeMs)}. Take a ${settings.breakLengthMinutes} minute break.`;
    this.deps.emitReminder({ kind: 'break', message });
    void this.deps.notifier.notify('Time for a break', message);
  }

  // --- downtime -----------------------------------------------------------

  private checkDowntime(now: number, settings: Settings): void {
    if (!settings.downtimeEnabled) return;
    const state = this.deps.state();
    if (state.idle || state.paused || !state.currentAppId) return;

    const minute = minutesIntoDay(now);
    if (
      !inDailyWindow(minute, parseClock(settings.downtimeStart), parseClock(settings.downtimeEnd))
    ) {
      return;
    }
    if (now - this.lastDowntimeNotice < DOWNTIME_REPEAT_MS) return;

    this.lastDowntimeNotice = now;
    const message = `Downtime is on until ${settings.downtimeEnd}. Time to wind down.`;
    this.deps.emitReminder({ kind: 'downtime', message });
    void this.deps.notifier.notify('Downtime', message);
  }

  // --- focus mode ---------------------------------------------------------

  private checkFocusMode(appId: string): void {
    if (this.focusUntil === null) return;
    const now = this.deps.now();
    if (now - this.lastFocusNudge < FOCUS_NUDGE_REPEAT_MS) return;

    // Distracting = a category explicitly marked unproductive (productive = 0).
    if (this.categoryOf(appId)?.productive !== 0) return;

    this.lastFocusNudge = now;
    const message = `${this.appName(appId)} is distracting. Focus mode is on.`;
    this.deps.emitReminder({ kind: 'focus', message });
    void this.deps.notifier.notify('Stay focused', message);
  }

  private checkFocusExpiry(now: number): void {
    if (this.focusUntil === null || now < this.focusUntil) return;
    this.focusUntil = null;
    const message = 'Focus session finished. Nice work.';
    this.deps.emitReminder({ kind: 'focus', message });
    void this.deps.notifier.notify('Focus mode ended', message);
  }

  // --- lookups ------------------------------------------------------------

  private categoryOf(appId: string): { name: string; productive: number | null } | undefined {
    const row = this.deps.db
      .query(
        `SELECT categories.name, categories.productive FROM apps
         JOIN categories ON categories.id = apps.category_id WHERE apps.app_id = ?`,
      )
      .get(appId) as { name: string; productive: number | null } | null;
    return row ?? undefined;
  }

  private appName(appId: string): string {
    const row = this.deps.db.query('SELECT name FROM apps WHERE app_id = ?').get(appId) as {
      name: string | null;
    } | null;
    return row?.name ?? appId;
  }

  private displayName(limit: Limit): string {
    return limit.targetType === 'app' ? this.appName(limit.target) : limit.target;
  }
}

function withinSchedule(schedule: string | undefined, now: number): boolean {
  if (!schedule) return true;
  const match = /^(\d{2}:\d{2})-(\d{2}:\d{2})$/.exec(schedule);
  if (!match?.[1] || !match[2]) return true;
  return inDailyWindow(minutesIntoDay(now), parseClock(match[1]), parseClock(match[2]));
}

export function formatDuration(ms: number): string {
  const totalMinutes = Math.round(ms / 60_000);
  const h = Math.floor(totalMinutes / 60);
  const m = totalMinutes % 60;
  if (h === 0) return `${m} min`;
  return m === 0 ? `${h} h` : `${h} h ${m} min`;
}
