import { IPC_VERSION, runtimeSocketPath } from '@screentime/shared';

type Sock = Awaited<ReturnType<typeof Bun.connect>>;

type Pending = {
  resolve: (v: unknown) => void;
  reject: (e: Error) => void;
  timer: ReturnType<typeof setTimeout>;
};

const CALL_TIMEOUT_MS = 10_000;

/**
 * Minimal JSON-RPC 2.0 client for the daemon's Unix socket. Used from the
 * Electrobun main process, which bridges results to the webview.
 */
export class DaemonClient {
  /** Server → client notifications (`event.focus`, `event.limitHit`, ...). */
  onNotification: ((method: string, params: unknown) => void) | undefined;
  /** Called once when an established connection drops. */
  onClose: (() => void) | undefined;

  private socket: Sock | undefined;
  private nextId = 1;
  private pending = new Map<number, Pending>();
  private buffer = '';

  get connected(): boolean {
    return this.socket !== undefined;
  }

  async connect(path = runtimeSocketPath()): Promise<void> {
    this.buffer = '';
    // The handlers need to know which socket they belong to, but can't refer to
    // the const being initialised; a holder keeps the types non-circular.
    const self: { socket?: Sock } = {};
    self.socket = await Bun.connect({
      unix: path,
      socket: {
        data: (_socket, chunk) => this.handleData(chunk.toString('utf8')),
        close: () => this.handleClose(self.socket),
        error: () => this.handleClose(self.socket),
      },
    });
    this.socket = self.socket;

    try {
      await this.call('version', { major: IPC_VERSION });
    } catch (err) {
      this.close();
      throw err;
    }
  }

  async call<T = unknown>(method: string, params?: unknown): Promise<T> {
    const socket = this.socket;
    if (!socket) throw new Error('not connected to the daemon');

    const id = this.nextId++;
    return new Promise<T>((resolve, reject) => {
      const timer = setTimeout(() => {
        this.pending.delete(id);
        reject(new Error(`daemon did not answer ${method} in time`));
      }, CALL_TIMEOUT_MS);
      this.pending.set(id, { resolve: resolve as (v: unknown) => void, reject, timer });
      socket.write(`${JSON.stringify({ jsonrpc: '2.0', id, method, params })}\n`);
    });
  }

  close(): void {
    const socket = this.socket;
    this.socket = undefined;
    socket?.end();
    this.failPending(new Error('connection closed'));
  }

  private handleClose(socket: unknown): void {
    // Ignore stale callbacks from a socket we already replaced or closed.
    if (this.socket !== socket) return;
    this.socket = undefined;
    this.failPending(new Error('daemon connection closed'));
    this.onClose?.();
  }

  private failPending(error: Error): void {
    for (const waiter of this.pending.values()) {
      clearTimeout(waiter.timer);
      waiter.reject(error);
    }
    this.pending.clear();
  }

  private handleData(chunk: string): void {
    this.buffer += chunk;
    let newlineIndex: number;
    // biome-ignore lint/suspicious/noAssignInExpressions: streaming line parser
    while ((newlineIndex = this.buffer.indexOf('\n')) !== -1) {
      const line = this.buffer.slice(0, newlineIndex);
      this.buffer = this.buffer.slice(newlineIndex + 1);
      if (!line.trim()) continue;

      let message: {
        id?: number;
        method?: string;
        params?: unknown;
        result?: unknown;
        error?: { message: string };
      };
      try {
        message = JSON.parse(line);
      } catch {
        continue;
      }

      if (message.id === undefined || message.id === null) {
        if (message.method) this.onNotification?.(message.method, message.params);
        continue;
      }

      const waiter = this.pending.get(message.id);
      if (!waiter) continue;
      this.pending.delete(message.id);
      clearTimeout(waiter.timer);
      if (message.error) waiter.reject(new Error(message.error.message));
      else waiter.resolve(message.result);
    }
  }
}
