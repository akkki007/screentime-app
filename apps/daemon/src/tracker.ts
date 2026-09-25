import type { Database } from 'bun:sqlite';
import type { FocusProvider, FocusedWindow } from '@screentime/shared';

const HEARTBEAT_MS = 5_000;
const MERGE_GAP_MS = 10_000;
const DEFAULT_IDLE_THRESHOLD_MS = 3 * 60 * 1_000;

type OpenSession = {
  rowId: number;
  appId: string;
  appRowId: number;
  title?: string;
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
  private idle = false;
  private heartbeat: ReturnType<typeof setInterval> | undefined;
  private unsubscribeFocus: (() => void) | undefined;
  private unsubscribeIdle: (() => void) | undefined;

  constructor(
    private readonly db: Database,
    private readonly focusProvider: FocusProvider,
    private readonly idleThresholdMs = DEFAULT_IDLE_THRESHOLD_MS,
  ) {}

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

  private handleFocusChange(w: FocusedWindow): void {
    if (this.idle) return;

    if (this.open && this.open.appId === w.appId && w.ts - this.open.lastSeenTs < MERGE_GAP_MS) {
      this.open.lastSeenTs = w.ts;
      this.open.title = w.title ?? this.open.title;
      this.touchOpenSession(w.ts);
      return;
    }

    this.closeOpenSession();
    this.openSession(w);
  }

  private handleIdleChange(idle: boolean): void {
    this.idle = idle;
    if (idle) {
      this.closeOpenSession();
    }
  }

  private handleHeartbeat(): void {
    if (!this.open || this.idle) return;
    this.touchOpenSession(Date.now());
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
      appRowId,
      title: w.title,
      startTs: w.ts,
      lastSeenTs: w.ts,
    };
  }

  private touchOpenSession(ts: number): void {
    if (!this.open) return;
    this.open.lastSeenTs = ts;
    this.db.query('UPDATE sessions SET end_ts = ? WHERE id = ?').run(ts, this.open.rowId);
  }

  private closeOpenSession(): void {
    if (!this.open) return;
    this.db
      .query('UPDATE sessions SET end_ts = ? WHERE id = ?')
      .run(this.open.lastSeenTs, this.open.rowId);
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
