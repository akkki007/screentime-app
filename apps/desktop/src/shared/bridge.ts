import type { RPCSchema } from 'electrobun';

/**
 * RPC between the Bun main process and the Svelte webview. The webview never
 * talks to the daemon directly: every daemon call goes through the single
 * `daemon` request, so the main process is the only place that owns the socket.
 */
export type BridgeSchema = {
  bun: RPCSchema<{
    requests: {
      /** Forward a JSON-RPC call to the daemon (allow-listed to known methods). */
      daemon: { params: { method: string; params?: unknown }; response: unknown };
      /** Write a data export to ~/Downloads and reveal it. Returns the saved path. */
      saveExport: {
        params: { format: 'csv' | 'json'; from?: number; to?: number };
        response: { path: string };
      };
      /** Current daemon connection state, for the first paint. */
      connectionState: { params: undefined; response: ConnectionState };
    };
    messages: Record<never, never>;
  }>;
  webview: RPCSchema<{
    requests: Record<never, never>;
    messages: {
      /** A daemon → UI notification such as `event.status` or `event.limitHit`. */
      daemonEvent: { name: string; payload: unknown };
      connection: ConnectionState;
    };
  }>;
};

export type ConnectionState = { connected: boolean; error?: string };
