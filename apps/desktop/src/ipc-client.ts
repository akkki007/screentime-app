import { IPC_VERSION, runtimeSocketPath } from '@screentime/shared';

/**
 * Minimal JSON-RPC 2.0 client for talking to the daemon over its Unix
 * socket. The Electrobun UI (Phase 2) will use this from the main/bun
 * process and bridge results to the webview.
 */
export class DaemonClient {
  private socket: Awaited<ReturnType<typeof Bun.connect>> | undefined;
  private nextId = 1;
  private pending = new Map<
    number,
    { resolve: (v: unknown) => void; reject: (e: Error) => void }
  >();
  private buffer = '';

  async connect(path = runtimeSocketPath()): Promise<void> {
    this.socket = await Bun.connect({
      unix: path,
      socket: {
        data: (_socket, chunk) => this.handleData(chunk.toString('utf8')),
        error: (_socket, err) => console.error('[ipc-client] socket error:', err),
      },
    });
    await this.call('version', { major: IPC_VERSION });
  }

  async call<T = unknown>(method: string, params?: unknown): Promise<T> {
    if (!this.socket) throw new Error('not connected');
    const id = this.nextId++;
    const message = { jsonrpc: '2.0', id, method, params };
    return new Promise<T>((resolve, reject) => {
      this.pending.set(id, { resolve: resolve as (v: unknown) => void, reject });
      this.socket?.write(`${JSON.stringify(message)}\n`);
    });
  }

  private handleData(chunk: string): void {
    this.buffer += chunk;
    let newlineIndex: number;
    // biome-ignore lint/suspicious/noAssignInExpressions: streaming line parser
    while ((newlineIndex = this.buffer.indexOf('\n')) !== -1) {
      const line = this.buffer.slice(0, newlineIndex);
      this.buffer = this.buffer.slice(newlineIndex + 1);
      if (!line.trim()) continue;
      const res = JSON.parse(line);
      const waiter = this.pending.get(res.id);
      if (!waiter) continue;
      this.pending.delete(res.id);
      if (res.error) waiter.reject(new Error(res.error.message));
      else waiter.resolve(res.result);
    }
  }
}
