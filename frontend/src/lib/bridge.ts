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

type HostWindow = {
  _wails?: { environment?: unknown };
  webkit?: { messageHandlers?: { external?: unknown } };
  chrome?: { webview?: unknown };
};

/**
 * Whether this page runs inside the app's webview. Wails sets
 * `_wails.environment`, but on Linux (WebKitGTK) only once the page has
 * finished loading, which is after this module first runs, so checking it
 * once raced and a window sometimes showed "couldn't start". The webview's
 * message channel (WebKit's `external` handler, or WebView2's
 * `chrome.webview`) exists from the start, so when it is there we wait for
 * the environment instead. A plain browser has neither and answers at once.
 */
export async function waitForApp(
  win: HostWindow,
  { timeoutMs = 5000, intervalMs = 10 } = {},
): Promise<boolean> {
  if (win._wails?.environment) return true;
  if (!win.webkit?.messageHandlers?.external && !win.chrome?.webview) return false;
  const deadline = Date.now() + timeoutMs;
  while (Date.now() < deadline) {
    await new Promise((r) => setTimeout(r, intervalMs));
    if (win._wails?.environment) return true;
  }
  return false;
}

export async function createBridge(): Promise<Bridge> {
  if (!(await waitForApp(window as HostWindow))) {
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
