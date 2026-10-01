import type { Database } from 'bun:sqlite';
import type { FocusProvider, FocusedWindow } from '@screentime/shared';
import { ensureApp } from './apps';

export const HEARTBEAT_MS = 5_000;
const MERGE_GAP_MS = 10_000;
const DEFAULT_IDLE_THRESHOLD_MS = 3 * 60 * 1_000;

export type TrackerOptions = {
  idleThresholdMs?: number;
  /** Window titles often contain private data, so they are dropped unless the user opts in. */
  captureTitles?: boolean;
  /** Injectable clock, for tests. */
  now?: () => number;
  /** Debug sink; never receives titles unless `captureTitles` is on. */
  log?: (message: string) => void;
};

export type TrackerEvent =
  | { type: 'focus'; appId: string; since: number }
  | { type: 'idle'; at: number }
  | { type: 'active'; at: number }
  | { type: 'paused'; until: number }
  | { type: 'resumed' };

export type TrackerState = {
  paused: boolean;
  resumeAt: number | null;
  idle: boolean;
  /** The app currently being counted, or null while idle, paused or unfocused. */
  currentAppId: string | null;
  since: number | null;
};

type OpenSession = {
  rowId: number;
  appId: string;
  startTs: number;
  lastSeenTs: number;
};

/**
 * Turns focus/idle events into `sessions` rows.
 *
 * Merge rule: same app, gap under MERGE_GAP_MS, and not idle -> extend the
 * open row's end_ts in place. Otherwise close it and open a new one.
 * A heartbeat re-extends end_ts every HEARTBEAT_MS so a crash loses at most
 * one heartbeat interval of data.
 */
export class Tracker {
  private open: OpenSession | undefined;
  /** Latest focused window, kept so tracking can resume after idle, pause or suspend. */
  private current: FocusedWindow | undefined;
  private idle = false;
  private pausedUntil: number | null = null;
  private heartbeat: ReturnType<typeof setInterval> | undefined;
  private unsubscribeFocus: (() => void) | undefined;
  private unsubscribeIdle: (() => void) | undefined;
  private readonly listeners = new Set<(event: TrackerEvent) => void>();

  private idleThresholdMs: number;
  private captureTitles: boolean;
  private readonly now: () => number;
  private readonly log: (message: string) => void;

  constructor(
    private readonly db: Database,
    private readonly focusProvider: FocusProvider,
    options: TrackerOptions = {},
  ) {
    this.idleThresholdMs = options.idleThresholdMs ?? DEFAULT_IDLE_THRESHOLD_MS;
    this.captureTitles = options.captureTitles ?? false;
    this.now = options.now ?? Date.now;
    this.log = options.log ?? (() => {});
  }

  start(): void {
    this.unsubscribeFocus = this.focusProvider.onFocusChange((w) => this.handleFocusChange(w));
    this.subscribeIdle();
    this.heartbeat = setInterval(() => this.handleHeartbeat(), HEARTBEAT_MS);
  }

  stop(): void {
    clearInterval(this.heartbeat);
    this.unsubscribeFocus?.();
    this.unsubscribeIdle?.();
    this.closeOpenSession();
  }

  /** Subscribe to state changes. Returns an unsubscribe function. */
  onEvent(listener: (event: TrackerEvent) => void): () => void {
    this.listeners.add(listener);
    return () => this.listeners.delete(listener);
  }

  state(): TrackerState {
    return {
      paused: this.pausedUntil !== null,
      resumeAt: this.pausedUntil,
      idle: this.idle,
      currentAppId: this.open?.appId ?? null,
      since: this.open?.startTs ?? null,
    };
  }

  /** Applies changed settings without dropping the focus subscription. */
  configure(options: { captureTitles?: boolean; idleThresholdMs?: number }): void {
    if (options.captureTitles !== undefined) this.captureTitles = options.captureTitles;

    const threshold = options.idleThresholdMs;
    if (threshold !== undefined && threshold !== this.idleThresholdMs) {
      this.idleThresholdMs = threshold;
      // Changing a setting needs the user at the keyboard, so they are not idle.
      this.handleIdleChange(false);
      this.unsubscribeIdle?.();
      this.subscribeIdle();
    }
  }

  /** Stops counting time until `minutes` from now. Returns when it will resume. */
  pause(minutes: number): number {
    const until = this.now() + minutes * 60_000;
    this.pausedUntil = until;
    this.closeOpenSession();
    this.log(`paused until ${new Date(until).toISOString()}`);
    this.emit({ type: 'paused', until });
    return until;
  }

  resume(): void {
    if (this.pausedUntil === null) return;
    this.pausedUntil = null;
    this.log('resumed');
    if (!this.idle && this.current) this.openSession({ ...this.current, ts: this.now() });
    this.emit({ type: 'resumed' });
  }

  /**
   * Drops the open session without writing it and starts a fresh one from the
   * last known focus. Used after the database rows underneath it were wiped.
   */
  restartSession(): void {
    this.open = undefined;
    if (!this.idle && this.pausedUntil === null && this.current) {
      this.openSession({ ...this.current, ts: this.now() });
    }
  }

  private subscribeIdle(): void {
    this.unsubscribeIdle = this.focusProvider.onIdleChange(
      (idle) => this.handleIdleChange(idle),
      this.idleThresholdMs,
    );
  }

  private emit(event: TrackerEvent): void {
    for (const listener of this.listeners) listener(event);
  }

  private handleFocusChange(raw: FocusedWindow): void {
    const w: FocusedWindow = {
      ...raw,
      appId: normalizeAppId(raw.appId),
      title: this.captureTitles ? raw.title : undefined,
    };
    const appChanged = this.current?.appId !== w.appId;
    this.current = w;
    this.log(`focus -> ${w.appId}${w.title ? ` "${w.title}"` : ''}${this.idle ? ' (idle)' : ''}`);
    if (appChanged) this.emit({ type: 'focus', appId: w.appId, since: w.ts });

    if (this.idle || this.pausedUntil !== null) return;

    if (this.open && this.open.appId === w.appId && w.ts - this.open.lastSeenTs < MERGE_GAP_MS) {
      this.touchOpenSession(w.ts);
      return;
    }

    // The user was in the old app right up to this switch, so end it here
    // rather than at the last heartbeat (up to 5 s earlier) — unless the gap
    // is so long that the machine must have been suspended.
    if (this.open && w.ts - this.open.lastSeenTs < MERGE_GAP_MS) this.touchOpenSession(w.ts);
    this.closeOpenSession();
    this.openSession(w);
  }

  private handleIdleChange(idle: boolean): void {
    if (idle === this.idle) return;
    this.idle = idle;
    this.log(idle ? 'idle' : 'active');

    const now = this.now();
    if (idle) {
      // The idle watch fires after `idleThresholdMs` of no input, but the
      // heartbeat kept extending the session through that stretch. Idle
      // actually began a threshold ago, so don't count that time.
      const since = now - this.idleThresholdMs;
      this.closeOpenSession(since);
      this.emit({ type: 'idle', at: since });
    } else {
      // Returning to the same window sends no focus event, so resume from the
      // last known one.
      if (this.current && this.pausedUntil === null) {
        this.openSession({ ...this.current, ts: now });
      }
      this.emit({ type: 'active', at: now });
    }
  }

  private handleHeartbeat(): void {
    const now = this.now();

    // Checked here rather than with a timer so an expiry that passes during
    // suspend is still honoured on wake.
    if (this.pausedUntil !== null && now >= this.pausedUntil) this.resume();

    if (!this.open || this.idle) return;

    if (now - this.open.lastSeenTs > MERGE_GAP_MS) {
      // Heartbeats stopped for far longer than their interval: the machine was
      // suspended (or the daemon stalled). Don't stretch the session across the
      // gap; end it where we last saw activity and start a fresh one.
      this.log('gap detected (suspend?), splitting session');
      this.closeOpenSession();
      if (this.current) this.openSession({ ...this.current, ts: now });
      return;
    }
    this.touchOpenSession(now);
  }

  private openSession(w: FocusedWindow): void {
    const appRowId = ensureApp(this.db, w.appId);
    const result = this.db
      .query(
        `INSERT INTO sessions (app_id, title, start_ts, end_ts, source) VALUES (?, ?, ?, ?, 'desktop')`,
      )
      .run(appRowId, w.title ?? null, w.ts, w.ts);

    this.open = {
      rowId: Number(result.lastInsertRowid),
      appId: w.appId,
      startTs: w.ts,
      lastSeenTs: w.ts,
    };
  }

  private touchOpenSession(ts: number): void {
    if (!this.open) return;
    this.open.lastSeenTs = ts;
    this.db.query('UPDATE sessions SET end_ts = ? WHERE id = ?').run(ts, this.open.rowId);
  }

  private closeOpenSession(endTs?: number): void {
    if (!this.open) return;
    const end = Math.max(
      this.open.startTs,
      Math.min(this.open.lastSeenTs, endTs ?? Number.POSITIVE_INFINITY),
    );
    this.db.query('UPDATE sessions SET end_ts = ? WHERE id = ?').run(end, this.open.rowId);
    this.open = undefined;
  }
}

/**
 * GNOME reports `org.mozilla.firefox.desktop`; the docs identify apps without
 * the suffix. Synthetic per-window IDs (`window:28`, which GNOME makes up for
 * windows with no .desktop file) would create a new "app" every time, so they
 * are folded into one `unknown` bucket.
 */
export function normalizeAppId(appId: string): string {
  if (/^window:\d+$/.test(appId)) return 'unknown';
  return appId.endsWith('.desktop') ? appId.slice(0, -'.desktop'.length) : appId;
}
