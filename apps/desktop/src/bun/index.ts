import { existsSync } from 'node:fs';
import { mkdir, writeFile } from 'node:fs/promises';
import { homedir } from 'node:os';
import { basename, extname, join } from 'node:path';
import { RpcMethods } from '@screentime/shared';
import type { DataExportResponse, TrackerStatus } from '@screentime/shared';
import { BrowserView, BrowserWindow, Screen, Tray, Updater, Utils } from 'electrobun/main';
import type { BridgeSchema, ConnectionState } from '../shared/bridge';
import { DaemonConnection } from './daemon-connection';
import { buildTrayMenu, parseTrayAction, trayIconFor, trayTitle } from './tray-menu';

const DEV_SERVER_URL = 'http://localhost:5173';

type BridgeRpc = ReturnType<typeof BrowserView.defineRPC<BridgeSchema>>;

let mainWindow: BrowserWindow<BridgeRpc> | undefined;
let widgetWindow: BrowserWindow<BridgeRpc> | undefined;
/** Today's tracked total, for the tray menu. Refreshed on status changes and every minute. */
let todayMs: number | undefined;

/** Sends a message to every open window (dashboard and quick panel). */
function broadcast(send: (rpc: NonNullable<BrowserWindow<BridgeRpc>['webview']['rpc']>) => void) {
  for (const w of [mainWindow, widgetWindow]) {
    const rpc = w?.webview.rpc;
    if (rpc) send(rpc);
  }
}
let status: TrackerStatus | undefined;
let state: ConnectionState = { connected: false };

const daemon: DaemonConnection = new DaemonConnection({
  onNotification: (name, payload) => {
    if (name === 'event.status') {
      status = payload as TrackerStatus;
      refreshTray();
      void refreshToday();
    }
    broadcast((rpc) => rpc.send.daemonEvent({ name, payload }));
  },
  onState: (next) => {
    state = next;
    if (!next.connected) status = undefined;
    else
      void daemon.call<TrackerStatus>('tracker.status').then(
        (s) => {
          status = s;
          refreshTray();
        },
        () => {},
      );
    refreshTray();
    broadcast((rpc) => rpc.send.connection(next));
  },
});

/** Only methods the daemon actually defines may be forwarded from the webview. */
function isDaemonMethod(method: string): boolean {
  return Object.hasOwn(RpcMethods, method);
}

const rpc: BridgeRpc = BrowserView.defineRPC<BridgeSchema>({
  handlers: {
    requests: {
      daemon: ({ method, params }) => {
        if (!isDaemonMethod(method)) throw new Error(`unknown daemon method: ${method}`);
        return daemon.call(method, params);
      },
      connectionState: () => state,
      openDashboard: async () => {
        widgetWindow?.close();
        await openWindow();
      },
      quitApp: () => {
        daemon.stop();
        Utils.quit();
        return undefined;
      },
      saveExport: async ({ format, from, to }) => {
        const result = await daemon.call<DataExportResponse>('data.export', { format, from, to });
        const path = await saveToDownloads(result.filename, result.content);
        Utils.showItemInFolder(path);
        return { path };
      },
    },
    messages: {},
  },
});

/** Writes into ~/Downloads without ever overwriting an existing file. */
async function saveToDownloads(filename: string, content: string): Promise<string> {
  const dir = join(homedir(), 'Downloads');
  await mkdir(dir, { recursive: true });

  // The daemon names the file, but never trust a name to stay inside `dir`.
  const safe = basename(filename);
  const ext = extname(safe);
  const stem = safe.slice(0, safe.length - ext.length);

  let path = join(dir, safe);
  for (let n = 1; existsSync(path); n++) path = join(dir, `${stem} (${n})${ext}`);
  await writeFile(path, content, { flag: 'wx' });
  return path;
}

async function viewUrl(): Promise<string> {
  if ((await Updater.localInfo.channel()) === 'dev') {
    try {
      await fetch(DEV_SERVER_URL, { method: 'HEAD' });
      console.log(`HMR enabled: using Vite dev server at ${DEV_SERVER_URL}`);
      return DEV_SERVER_URL;
    } catch {
      // no dev server: use the bundled build
    }
  }
  return 'views://mainview/index.html';
}

async function openWindow(): Promise<void> {
  if (mainWindow) {
    mainWindow.show();
    return;
  }
  const created = new BrowserWindow({
    title: 'Screentime',
    url: await viewUrl(),
    frame: { width: 1120, height: 780, x: 160, y: 120 },
    rpc,
  });
  created.on('close', () => {
    if (mainWindow === created) mainWindow = undefined;
  });
  mainWindow = created;
}

const WIDGET = { width: 372, height: 624 };

/**
 * The quick panel: a small frameless glass window, the nearest thing to a
 * popover that a GNOME tray icon allows (its own menu can't be styled). It
 * closes when it loses focus. Wayland compositors ignore requested positions,
 * so on GNOME it appears where the shell puts it.
 */
async function toggleWidget(): Promise<void> {
  if (widgetWindow) {
    widgetWindow.close();
    return;
  }
  const area = Screen.getPrimaryDisplay().workArea;
  const created = new BrowserWindow({
    title: 'Screentime quick panel',
    url: `${await viewUrl()}#widget`,
    frame: {
      width: WIDGET.width,
      height: WIDGET.height,
      x: Math.max(area.x, area.x + area.width - WIDGET.width - 12),
      y: area.y + 8,
    },
    titleBarStyle: 'hidden',
    transparent: true,
    rpc,
  });
  created.setAlwaysOnTop(true);

  const openedAt = Date.now();
  created.on('blur', () => {
    // Ignore the focus churn while the window is still being mapped.
    if (Date.now() - openedAt > 700) created.close();
  });
  created.on('close', () => {
    if (widgetWindow === created) widgetWindow = undefined;
  });
  widgetWindow = created;
}

async function refreshToday(): Promise<void> {
  if (!state.connected) return;
  try {
    const midnight = new Date().setHours(0, 0, 0, 0);
    const rows = await daemon.call<{ ms: number }[]>('usage.summary', {
      from: midnight,
      to: Date.now() + 60_000,
      groupBy: 'app',
    });
    todayMs = rows.reduce((n, r) => n + r.ms, 0);
    refreshTray();
  } catch {
    // the menu just keeps its last value
  }
}
setInterval(() => void refreshToday(), 60_000);

// --- tray ------------------------------------------------------------------

let tray: Tray | undefined;
try {
  tray = new Tray({
    title: 'Screentime',
    image: 'views://assets/tray.png',
    template: false,
    width: 22,
    height: 22,
  });
  tray.on('tray-clicked', (event) => {
    const action = parseTrayAction((event as { data?: { action?: string } }).data?.action);
    if (action) void handleTrayAction(action);
  });
} catch (err) {
  // Some desktops have no tray (GNOME needs the AppIndicator extension).
  console.warn('[tray] unavailable:', err);
}

let lastIcon = '';

function refreshTray(): void {
  if (!tray) return;
  tray.setTitle(trayTitle(status, state.connected));

  const icon = trayIconFor(status, state.connected);
  if (icon !== lastIcon) {
    lastIcon = icon;
    tray.setImage(`views://assets/${icon}.png`);
  }

  const resume = status?.resumeAt ? new Date(status.resumeAt) : undefined;
  tray.setMenu(
    buildTrayMenu(status, state.connected, {
      appName: status?.currentAppId ? appName(status.currentAppId) : undefined,
      todayLabel: todayMs === undefined ? undefined : shortDuration(todayMs),
      resumeLabel: resume?.toLocaleTimeString([], {
        hour: '2-digit',
        minute: '2-digit',
        hour12: false,
      }),
    }),
  );
}

const appNames = new Map<string, string>();

/** A readable name for an app ID, from the daemon's app list (fetched once per new app). */
function appName(appId: string): string {
  const known = appNames.get(appId);
  if (known) return known;
  const fallback = (appId.split('.').at(-1) ?? appId).split('_')[0] ?? appId;
  const label = fallback.charAt(0).toUpperCase() + fallback.slice(1);
  appNames.set(appId, label);
  void daemon
    .call<{ appId: string; name: string | null }[]>('apps.list')
    .then((apps) => {
      for (const a of apps) if (a.name) appNames.set(a.appId, a.name);
      refreshTray();
    })
    .catch(() => {});
  return label;
}

function shortDuration(ms: number): string {
  const minutes = Math.round(ms / 60_000);
  const h = Math.floor(minutes / 60);
  return h === 0 ? `${minutes}m` : `${h}h ${String(minutes % 60).padStart(2, '0')}m`;
}

async function handleTrayAction(
  action: NonNullable<ReturnType<typeof parseTrayAction>>,
): Promise<void> {
  try {
    switch (action.kind) {
      case 'open':
        await openWindow();
        break;
      case 'widget':
        await toggleWidget();
        break;
      case 'pause':
        await daemon.call('tracker.pause', { minutes: action.minutes });
        break;
      case 'resume':
        await daemon.call('tracker.resume');
        break;
      case 'focus':
        await daemon.call('focus.start', { minutes: action.minutes });
        break;
      case 'focus-stop':
        await daemon.call('focus.stop');
        break;
      case 'quit':
        daemon.stop();
        Utils.quit();
        break;
    }
  } catch (err) {
    console.error(`[tray] ${action.kind} failed:`, err);
  }
}

refreshTray();
daemon.start();
void refreshToday();
await openWindow();
// Development aid: open the quick panel at startup (the tray can't be clicked from a script).
if (process.env.SCREENTIME_OPEN_WIDGET === '1') await toggleWidget();
console.log('Screentime UI started');
