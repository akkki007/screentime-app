import type { Database } from 'bun:sqlite';
import type { BrowserActiveTab } from '@screentime/shared';

const MERGE_GAP_MS = 10_000;

type OpenWebSession = { rowId: number; domain: string; startTs: number; lastSeenTs: number };

/**
 * Turns the browser extension's "active tab" reports into `web_sessions` rows.
 *
 * A browser only reports when the tab changes, never on a timer, so time is
 * extended by the daemon's own heartbeat — and only while a browser is the
 * app actually being counted (focused, not idle, not paused). The last
 * reported domain is remembered so coming back to the browser resumes it.
 */
export class WebTracker {
  private open: OpenWebSession | undefined;
  private currentDomain: string | undefined;

  constructor(
    private readonly db: Database,
    private readonly now: () => number = Date.now,
  ) {}

  /** Domain of the page currently being counted, if any. */
  get activeDomain(): string | undefined {
    return this.open?.domain;
  }

  /** A report from the extension. */
  handle(tab: BrowserActiveTab, tracking: boolean): void {
    this.currentDomain = tab.active ? tab.domain : undefined;
    this.sync(tracking);
  }

  /** Reconcile with whether a browser is currently being counted. Call on every heartbeat. */
  sync(tracking: boolean): void {
    const now = this.now();

    if (this.open) {
      if (now - this.open.lastSeenTs > MERGE_GAP_MS) {
        // Heartbeats stalled (suspend): end where we last saw it, don't stretch.
        this.close();
      } else {
        // The user was on this page right up to now, even if they are leaving it.
        this.open.lastSeenTs = now;
      }
    }

    if (this.open && (!tracking || this.open.domain !== this.currentDomain)) {
      this.close();
    }

    if (!this.open && tracking && this.currentDomain) {
      const result = this.db
        .query('INSERT INTO web_sessions (domain, start_ts, end_ts) VALUES (?, ?, ?)')
        .run(this.currentDomain, now, now);
      this.open = {
        rowId: Number(result.lastInsertRowid),
        domain: this.currentDomain,
        startTs: now,
        lastSeenTs: now,
      };
      return;
    }

    if (this.open) {
      this.open.lastSeenTs = now;
      this.db.query('UPDATE web_sessions SET end_ts = ? WHERE id = ?').run(now, this.open.rowId);
    }
  }

  /** Ends the open session at `ts` (e.g. when idleness actually began). */
  closeAt(ts: number): void {
    if (!this.open) return;
    this.open.lastSeenTs = Math.max(this.open.startTs, Math.min(this.open.lastSeenTs, ts));
    this.close();
  }

  /** Drops the open session without writing it, after its rows were wiped. */
  reset(): void {
    this.open = undefined;
  }

  private close(): void {
    if (!this.open) return;
    this.db
      .query('UPDATE web_sessions SET end_ts = ? WHERE id = ?')
      .run(this.open.lastSeenTs, this.open.rowId);
    this.open = undefined;
  }
}
