import { APP_ID } from '@screentime/shared';
import type { FocusProvider, FocusedWindow, Unsubscribe } from '@screentime/shared';
import { parseTuple } from '../gvariant';
import { type Runner, bunRunner } from '../runner';

const FOCUS_INTERFACE = `${APP_ID}.Focus`;
const FOCUS_OBJECT_PATH = `/${APP_ID.replaceAll('.', '/')}/Focus`;
const IDLE_SERVICE = 'org.gnome.Mutter.IdleMonitor';
const IDLE_PATH = '/org/gnome/Mutter/IdleMonitor/Core';
const IDLE_POLL_MS = 3_000;
const RESTART_MS = 3_000;

const sleep = (ms: number) => new Promise((resolve) => setTimeout(resolve, ms));

/**
 * Focus source: our GNOME Shell extension (adapters/gnome-extension), which
 * emits `FocusChanged(s appId, s title, u pid)` and answers `GetFocus()`.
 * Idle source: Mutter's IdleMonitor `GetIdletime`.
 *
 * D-Bus is reached through the `gdbus` tool rather than a JavaScript library:
 * `dbus-next` alone cost ~25 MB of resident memory in an always-on service,
 * which is a large part of the daemon's 60 MB budget (see ADR 6).
 */
export class GnomeWaylandFocusProvider implements FocusProvider {
  readonly id = 'gnome-wayland';

  constructor(
    private readonly runner: Runner = bunRunner,
    private readonly env: NodeJS.ProcessEnv = process.env,
    private readonly now: () => number = Date.now,
  ) {}

  async isAvailable(): Promise<boolean> {
    if (this.env.XDG_SESSION_TYPE !== 'wayland') return false;
    if (!this.env.XDG_CURRENT_DESKTOP?.toLowerCase().includes('gnome')) return false;
    return this.runner.has('gdbus');
  }

  onFocusChange(cb: (w: FocusedWindow) => void): Unsubscribe {
    let disposed = false;
    let stopMonitor: (() => void) | undefined;

    const emit = (values: ReturnType<typeof parseTuple>) => {
      const [appId, title, pid] = values ?? [];
      if (disposed || typeof appId !== 'string' || !appId) return;
      cb({
        appId,
        title: typeof title === 'string' && title ? title : undefined,
        pid: typeof pid === 'number' && pid ? pid : undefined,
        ts: this.now(),
      });
    };

    // The extension only emits on change, so ask for the window focused right
    // now. Older extension builds lack GetFocus; that just fails and is ignored.
    const fetchCurrent = async () => {
      const { stdout, ok } = await this.runner.run([
        'gdbus',
        'call',
        '--session',
        '--dest',
        APP_ID,
        '--object-path',
        FOCUS_OBJECT_PATH,
        '--method',
        `${FOCUS_INTERFACE}.GetFocus`,
      ]);
      if (ok) emit(parseTuple(stdout));
    };

    const onLine = (line: string) => {
      if (line.includes(`${FOCUS_INTERFACE}.FocusChanged`)) {
        emit(parseTuple(line.slice(line.indexOf('('))));
      } else if (line.includes(`The name ${APP_ID} is owned by`)) {
        // The extension (re)appeared: it loads in parallel with login and GNOME
        // deactivates it on the lock screen, so this is how we catch up.
        void fetchCurrent();
      } else if (line.includes('does not have an owner')) {
        console.warn(
          '[gnome-wayland] waiting for the Screentime Shell extension (is it enabled? GNOME also deactivates it while the screen is locked)',
        );
      }
    };

    // gdbus keeps running across owner changes; this loop only covers the tool itself exiting.
    (async () => {
      while (!disposed) {
        const monitor = this.runner.stream(
          ['gdbus', 'monitor', '--session', '--dest', APP_ID, '--object-path', FOCUS_OBJECT_PATH],
          onLine,
        );
        stopMonitor = monitor.stop;
        await monitor.exited;
        if (!disposed) await sleep(RESTART_MS);
      }
    })();

    return () => {
      disposed = true;
      stopMonitor?.();
    };
  }

  onIdleChange(cb: (idle: boolean) => void, thresholdMs: number): Unsubscribe {
    let idle = false;
    let disposed = false;
    let busy = false;

    const poll = async () => {
      if (busy) return;
      busy = true;
      try {
        const { stdout, ok } = await this.runner.run([
          'gdbus',
          'call',
          '--session',
          '--dest',
          IDLE_SERVICE,
          '--object-path',
          IDLE_PATH,
          '--method',
          `${IDLE_SERVICE}.GetIdletime`,
        ]);
        const idleMs = parseTuple(stdout)?.[0];
        if (disposed || !ok || typeof idleMs !== 'number') return;
        const nowIdle = idleMs >= thresholdMs;
        if (nowIdle !== idle) {
          idle = nowIdle;
          cb(idle);
        }
      } finally {
        busy = false;
      }
    };

    const timer = setInterval(() => void poll(), IDLE_POLL_MS);
    return () => {
      disposed = true;
      clearInterval(timer);
    };
  }
}
