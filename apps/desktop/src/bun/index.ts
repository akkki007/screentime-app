import { existsSync } from 'node:fs';
import { mkdir, writeFile } from 'node:fs/promises';
import { homedir } from 'node:os';
import { basename, extname, join } from 'node:path';
import { RpcMethods } from '@screentime/shared';
import type { DataExportResponse, TrackerStatus } from '@screentime/shared';
import { BrowserView, BrowserWindow, Tray, Updater, Utils } from 'electrobun/main';
import type { BridgeSchema, ConnectionState } from '../shared/bridge';
import { DaemonConnection } from './daemon-connection';
import { buildTrayMenu, parseTrayAction, trayTitle } from './tray-menu';

const DEV_SERVER_URL = 'http://localhost:5173';

type BridgeRpc = ReturnType<typeof BrowserView.defineRPC<BridgeSchema>>;

let window: BrowserWindow<BridgeRpc> | undefined;
let status: TrackerStatus | undefined;
let state: ConnectionState = { connected: false };

const daemon: DaemonConnection = new DaemonConnection({
  onNotification: (name, payload) => {
    if (name === 'event.status') {
      status = payload as TrackerStatus;
      refreshTray();
    }
    window?.webview.rpc?.send.daemonEvent({ name, payload });
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
    window?.webview.rpc?.send.connection(next);
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
  if (window) {
    window.show();
    return;
  }
  const created = new BrowserWindow({
    title: 'Screentime',
    url: await viewUrl(),
    frame: { width: 1120, height: 780, x: 160, y: 120 },
    rpc,
  });
  created.on('close', () => {
    if (window === created) window = undefined;
  });
  window = created;
}

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

function refreshTray(): void {
  if (!tray) return;
  tray.setTitle(trayTitle(status, state.connected));
  tray.setMenu(buildTrayMenu(status, state.connected));
}

async function handleTrayAction(
  action: NonNullable<ReturnType<typeof parseTrayAction>>,
): Promise<void> {
  try {
    switch (action.kind) {
      case 'open':
        await openWindow();
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
await openWindow();
console.log('Screentime UI started');
