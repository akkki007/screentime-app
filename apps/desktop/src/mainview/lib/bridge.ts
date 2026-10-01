import type { ConnectionState } from '../../shared/bridge';

/**
 * How the webview reaches the daemon. Inside Electrobun it goes through the
 * Bun main process; in a plain browser (dev, screenshots) it falls back to an
 * in-memory mock daemon so the UI can be developed without any backend.
 */
export type Bridge = {
  mode: 'electrobun' | 'mock';
  call(method: string, params?: unknown): Promise<unknown>;
  saveExport(args: { format: 'csv' | 'json'; from?: number; to?: number }): Promise<{
    path: string;
  }>;
  connection(): Promise<ConnectionState>;
  onEvent(handler: (name: string, payload: unknown) => void): void;
  onConnection(handler: (state: ConnectionState) => void): void;
};

export async function createBridge(): Promise<Bridge> {
  const inElectrobun = typeof (window as { __electrobun?: unknown }).__electrobun !== 'undefined';
  if (!inElectrobun) {
    const { createMockBridge } = await import('./mock');
    return createMockBridge();
  }

  // Loaded lazily: the view runtime assumes the Electrobun host exists.
  const { Electroview } = await import('electrobun/view');
  type Schema = import('../../shared/bridge').BridgeSchema;

  let eventHandler: (name: string, payload: unknown) => void = () => {};
  let connectionHandler: (state: ConnectionState) => void = () => {};

  const rpc = Electroview.defineRPC<Schema>({
    handlers: {
      requests: {},
      messages: {
        daemonEvent: ({ name, payload }) => eventHandler(name, payload),
        connection: (state) => connectionHandler(state),
      },
    },
  });
  const view = new Electroview({ rpc });
  const request = view.rpc?.request;
  if (!request) throw new Error('Electrobun RPC is unavailable');

  return {
    mode: 'electrobun',
    call: (method, params) => request.daemon({ method, params }),
    saveExport: (args) => request.saveExport(args),
    connection: () => request.connectionState(undefined),
    onEvent: (h) => {
      eventHandler = h;
    },
    onConnection: (h) => {
      connectionHandler = h;
    },
  };
}
