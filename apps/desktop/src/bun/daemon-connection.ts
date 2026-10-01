import type { ConnectionState } from '../shared/bridge';
import { DaemonClient } from './ipc-client';

export type DaemonConnectionOptions = {
  socketPath?: string;
  /** Delay between reconnect attempts. */
  retryMs?: number;
  onNotification: (method: string, params: unknown) => void;
  onState: (state: ConnectionState) => void;
};

/**
 * Keeps a DaemonClient connected for the life of the UI. The daemon can start
 * after the UI, restart under it, or be stopped, and the dashboard should
 * simply catch up when it returns.
 */
export class DaemonConnection {
  private client: DaemonClient | undefined;
  private timer: ReturnType<typeof setTimeout> | undefined;
  private stopped = true;
  private lastError: string | undefined;

  constructor(private readonly options: DaemonConnectionOptions) {}

  get connected(): boolean {
    return this.client?.connected ?? false;
  }

  start(): void {
    if (!this.stopped) return;
    this.stopped = false;
    void this.attempt();
  }

  stop(): void {
    this.stopped = true;
    clearTimeout(this.timer);
    this.client?.close();
    this.client = undefined;
  }

  async call<T = unknown>(method: string, params?: unknown): Promise<T> {
    if (!this.client?.connected) {
      throw new Error('The Screentime daemon is not running. Start it with `bun run dev:daemon`.');
    }
    return this.client.call<T>(method, params);
  }

  private async attempt(): Promise<void> {
    if (this.stopped) return;

    const client = new DaemonClient();
    client.onNotification = this.options.onNotification;
    client.onClose = () => {
      if (this.client !== client) return;
      this.client = undefined;
      this.options.onState({ connected: false });
      this.scheduleRetry();
    };

    try {
      await client.connect(this.options.socketPath);
      if (this.stopped) {
        client.close();
        return;
      }
      this.client = client;
      this.lastError = undefined;
      this.options.onState({ connected: true });
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err);
      // Only report a changed error: a missing daemon would otherwise spam every retry.
      if (message !== this.lastError) {
        this.lastError = message;
        this.options.onState({ connected: false, error: message });
      }
      this.scheduleRetry();
    }
  }

  private scheduleRetry(): void {
    if (this.stopped) return;
    this.timer = setTimeout(() => void this.attempt(), this.options.retryMs ?? 2_000);
  }
}
