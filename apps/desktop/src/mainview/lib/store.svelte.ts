import type {
  AppInfo,
  Category,
  Limit,
  LimitHitEvent,
  ReminderEvent,
  Settings,
  TrackerStatus,
} from '@screentime/shared';
import { daemon, getBridge } from './api';

export type ViewId = 'today' | 'week' | 'apps' | 'limits' | 'wellbeing' | 'settings';

type Toast = { id: number; kind: 'info' | 'error' | 'success'; message: string };

class Store {
  ready = $state(false);
  connected = $state(false);
  connectionError = $state<string | undefined>();
  mode = $state<'electrobun' | 'mock'>('electrobun');

  view = $state<ViewId>('today');
  status = $state<TrackerStatus | undefined>();
  settings = $state<Settings | undefined>();
  categories = $state<Category[]>([]);
  apps = $state<AppInfo[]>([]);
  limits = $state<Limit[]>([]);

  /** Bumped when views should reload their data. */
  tick = $state(0);
  toasts = $state<Toast[]>([]);
  /** The limit whose overlay is showing, if any. */
  limitOverlay = $state<LimitHitEvent | undefined>();

  private nextToast = 1;

  /** Display lookups, derived from `apps` and `categories`. */
  appName = (appId: string): string | null =>
    this.apps.find((a) => a.appId === appId)?.name ?? null;

  categoryOf = (appId: string): Category | undefined => {
    const id = this.apps.find((a) => a.appId === appId)?.categoryId;
    return id == null ? undefined : this.categories.find((c) => c.id === id);
  };

  categoryColor = (name: string): string =>
    this.categories.find((c) => c.name === name)?.color ?? '#64748b';

  async init(): Promise<void> {
    const bridge = getBridge();
    this.mode = bridge.mode;

    bridge.onConnection((state) => {
      this.applyConnection(state.connected, state.error);
      if (state.connected) void this.loadAll();
    });
    bridge.onEvent((name, payload) => this.handleEvent(name, payload));

    const state = await bridge.connection();
    this.applyConnection(state.connected, state.error);
    if (state.connected) await this.loadAll();
    this.ready = true;
    console.info(
      `[ui] data source: ${bridge.mode}; daemon connected: ${state.connected}; apps: ${this.apps.length}`,
    );

    // Totals grow in the background; refresh views periodically.
    setInterval(() => {
      if (this.connected) this.tick++;
    }, 15_000);
  }

  private applyConnection(connected: boolean, error?: string): void {
    this.connected = connected;
    this.connectionError = connected ? undefined : error;
    if (!connected) this.status = undefined;
  }

  /** Reloads everything that isn't view-specific. */
  async loadAll(): Promise<void> {
    try {
      const [status, settings, categories, apps, limits] = await Promise.all([
        daemon('tracker.status'),
        daemon('settings.get'),
        daemon('categories.list'),
        daemon('apps.list'),
        daemon('limits.list'),
      ]);
      this.status = status;
      this.settings = settings;
      this.categories = categories;
      this.apps = apps;
      this.limits = limits;
      this.tick++;
    } catch (err) {
      this.notify('error', errorMessage(err));
    }
  }

  async refreshApps(): Promise<void> {
    this.apps = await daemon('apps.list');
  }

  async refreshLimits(): Promise<void> {
    this.limits = await daemon('limits.list');
  }

  private handleEvent(name: string, payload: unknown): void {
    switch (name) {
      case 'event.status':
        this.status = payload as TrackerStatus;
        // A new app may have appeared; keep the lookup tables fresh.
        if (
          this.status.currentAppId &&
          !this.apps.some((a) => a.appId === this.status?.currentAppId)
        ) {
          void this.refreshApps();
        }
        this.tick++;
        break;
      case 'event.limitHit': {
        const hit = payload as LimitHitEvent;
        if (hit.action !== 'notify') this.limitOverlay = hit;
        break;
      }
      case 'event.reminder': {
        const reminder = payload as ReminderEvent;
        this.notify('info', reminder.message);
        break;
      }
    }
  }

  notify(kind: Toast['kind'], message: string): void {
    const id = this.nextToast++;
    this.toasts = [...this.toasts, { id, kind, message }];
    setTimeout(() => this.dismissToast(id), kind === 'error' ? 8_000 : 4_500);
  }

  dismissToast(id: number): void {
    this.toasts = this.toasts.filter((t) => t.id !== id);
  }

  /** Runs an action, surfacing any failure as a toast. */
  async attempt<T>(action: () => Promise<T>, success?: string): Promise<T | undefined> {
    try {
      const result = await action();
      if (success) this.notify('success', success);
      return result;
    } catch (err) {
      this.notify('error', errorMessage(err));
      return undefined;
    }
  }
}

export function errorMessage(err: unknown): string {
  return err instanceof Error ? err.message : String(err);
}

export const store = new Store();
