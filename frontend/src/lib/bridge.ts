/**
 * How the webview reaches the daemon. Inside the app (Wails) every call goes
 * through the Go shell's `Bridge` service, which owns the daemon socket and
 * forwards only the UI's methods (ADR 3). In a plain browser (dev,
 * screenshots) it falls back to an in-memory mock daemon so the UI can be
 * developed without any backend.
 */
export type ConnectionState = { connected: boolean; error?: string };

export type Bridge = {
  mode: 'app' | 'mock';
  call(method: string, params?: unknown): Promise<unknown>;
  saveExport(args: { format: 'csv' | 'json'; from?: number; to?: number }): Promise<{
    path: string;
  }>;
  openDashboard(): Promise<void>;
  quitApp(): Promise<void>;
  /** Closes this window only; the tray and the daemon keep running. */
  closeWindow(): Promise<void>;
  connection(): Promise<ConnectionState>;
  onEvent(handler: (name: string, payload: unknown) => void): void;
  onConnection(handler: (state: ConnectionState) => void): void;
};

/** Go's fully qualified name for the bound service (internal/shell.Bridge). */
const BRIDGE = 'github.com/akkki007/screentime-app/internal/shell.Bridge';

/** Wails injects its environment before the page's scripts run. */
function insideApp(): boolean {
  return Boolean((window as { _wails?: { environment?: unknown } })._wails?.environment);
}

export async function createBridge(): Promise<Bridge> {
  if (!insideApp()) {
    // The in-memory mock is for browser previews only. This branch is removed
    // from production builds, so the mock never ships inside the app.
    if (!import.meta.env.DEV) throw new Error('Screentime must run inside its desktop app.');
    const { createMockBridge } = await import('./mock');
    return createMockBridge();
  }

  const { Call, Events } = await import('@wailsio/runtime');
  const call = <T>(method: string, ...args: unknown[]) =>
    Call.ByName(`${BRIDGE}.${method}`, ...args) as Promise<T>;

  let eventHandler: (name: string, payload: unknown) => void = () => {};
  let connectionHandler: (state: ConnectionState) => void = () => {};
  Events.On('daemon:event', (e) => {
    const { name, payload } = e.data as { name: string; payload: unknown };
    eventHandler(name, payload);
  });
  Events.On('daemon:connection', (e) => connectionHandler(e.data as ConnectionState));

  return {
    mode: 'app',
    call: (method, params) => call('Daemon', method, params ?? null),
    saveExport: ({ format, from, to }) => call('SaveExport', format, from ?? null, to ?? null),
    openDashboard: async () => void (await call('OpenDashboard')),
    quitApp: async () => void (await call('Quit')),
    closeWindow: async () => void (await call('CloseWindow')),
    connection: () => call('ConnectionState'),
    onEvent: (h) => {
      eventHandler = h;
    },
    onConnection: (h) => {
      connectionHandler = h;
    },
  };
}
