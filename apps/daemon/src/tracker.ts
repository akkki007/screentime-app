import type { Database } from 'bun:sqlite';
import type { FocusProvider, FocusedWindow } from '@screentime/shared';

const HEARTBEAT_MS = 5_000;
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
  /** Latest focused window, kept so tracking can resume after idle or suspend. */
  private current: FocusedWindow | undefined;
  private idle = false;
  private heartbeat: ReturnType<typeof setInterval> | undefined;
  private unsubscribeFocus: (() => void) | undefined;
  private unsubscribeIdle: (() => void) | undefined;

  private readonly idleThresholdMs: number;
  private readonly captureTitles: boolean;
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
    this.unsubscribeIdle = this.focusProvider.onIdleChange(
      (idle) => this.handleIdleChange(idle),
      this.idleThresholdMs,
    );
    this.heartbeat = setInterval(() => this.handleHeartbeat(), HEARTBEAT_MS);
  }

  stop(): void {
    clearInterval(this.heartbeat);
    this.unsubscribeFocus?.();
    this.unsubscribeIdle?.();
    this.closeOpenSession();
  }

  private handleFocusChange(raw: FocusedWindow): void {
    const w: FocusedWindow = {
      ...raw,
      appId: normalizeAppId(raw.appId),
      title: this.captureTitles ? raw.title : undefined,
    };
    this.current = w;
    this.log(`focus -> ${w.appId}${w.title ? ` "${w.title}"` : ''}${this.idle ? ' (idle)' : ''}`);

    if (this.idle) return;

    if (this.open && this.open.appId === w.appId && w.ts - this.open.lastSeenTs < MERGE_GAP_MS) {
      this.touchOpenSession(w.ts);
      return;
    }

    this.closeOpenSession();
    this.openSession(w);
  }

  private handleIdleChange(idle: boolean): void {
    if (idle === this.idle) return;
    this.idle = idle;
    this.log(idle ? 'idle' : 'active');

    if (idle) {
      // The idle watch fires after `idleThresholdMs` of no input, but the
      // heartbeat kept extending the session through that stretch. Idle
      // actually began a threshold ago, so don't count that time.
      this.closeOpenSession(this.now() - this.idleThresholdMs);
    } else if (this.current) {
      // Returning to the same window sends no focus event, so resume from the
      // last known one.
      this.openSession({ ...this.current, ts: this.now() });
    }
  }

  private handleHeartbeat(): void {
    if (!this.open || this.idle) return;

    const now = this.now();
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
    const appRowId = this.upsertApp(w.appId);
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

  private upsertApp(appId: string): number {
    this.db.query('INSERT OR IGNORE INTO apps (app_id) VALUES (?)').run(appId);
    const row = this.db.query('SELECT id FROM apps WHERE app_id = ?').get(appId) as {
      id: number;
    };
    return row.id;
  }
}

/** GNOME reports `org.mozilla.firefox.desktop`; the docs identify apps without the suffix. */
export function normalizeAppId(appId: string): string {
  return appId.endsWith('.desktop') ? appId.slice(0, -'.desktop'.length) : appId;
}
