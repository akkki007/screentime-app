import { APP_ID } from '@screentime/shared';
import type { FocusProvider, FocusedWindow, Unsubscribe } from '@screentime/shared';
import { sessionBus } from 'dbus-next';

const FOCUS_INTERFACE = `${APP_ID}.Focus`;
const FOCUS_OBJECT_PATH = `/${APP_ID.replaceAll('.', '/')}/Focus`;

const MUTTER_IDLE_SERVICE = 'org.gnome.Mutter.IdleMonitor';
const MUTTER_IDLE_PATH = '/org/gnome/Mutter/IdleMonitor/Core';
const MUTTER_IDLE_INTERFACE = 'org.gnome.Mutter.IdleMonitor';

/**
 * Focus source: our own GNOME Shell extension (adapters/gnome-extension),
 * which emits `${APP_ID}.Focus.FocusChanged(s appId, s title, u pid)` on the
 * session bus whenever the focused window changes.
 *
 * Idle source: org.gnome.Mutter.IdleMonitor, via a watch that fires once the
 * user has been idle for `thresholdMs`, re-armed on activity.
 */
export class GnomeWaylandFocusProvider implements FocusProvider {
  readonly id = 'gnome-wayland';

  async isAvailable(): Promise<boolean> {
    if (process.env.XDG_SESSION_TYPE !== 'wayland') return false;
    if (!process.env.XDG_CURRENT_DESKTOP?.toLowerCase().includes('gnome')) return false;

    try {
      const bus = sessionBus();
      await bus.getProxyObject('org.freedesktop.DBus', '/org/freedesktop/DBus');
      bus.disconnect();
      return true;
    } catch {
      return false;
    }
  }

  onFocusChange(cb: (w: FocusedWindow) => void): Unsubscribe {
    const bus = sessionBus();
    let disposed = false;

    (async () => {
      const obj = await bus.getProxyObject(APP_ID, FOCUS_OBJECT_PATH);
      const iface = obj.getInterface(FOCUS_INTERFACE);
      const handler = (appId: string, title: string, pid: number) => {
        if (disposed) return;
        cb({ appId, title: title || undefined, pid: pid || undefined, ts: Date.now() });
      };
      iface.on('FocusChanged', handler);

      // The extension only emits on change, so ask for the window that is
      // focused right now; otherwise nothing is tracked until the next switch.
      // Older extension builds lack GetFocus, so tolerate its absence.
      try {
        const [appId, title, pid] = (await iface.GetFocus?.()) as [string, string, number];
        if (appId) handler(appId, title, pid);
      } catch {
        // extension predates GetFocus
      }
    })().catch((err) => {
      console.error('[gnome-wayland] failed to subscribe to FocusChanged:', err);
    });

    return () => {
      disposed = true;
      bus.disconnect();
    };
  }

  onIdleChange(cb: (idle: boolean) => void, thresholdMs: number): Unsubscribe {
    const bus = sessionBus();
    let disposed = false;

    (async () => {
      const obj = await bus.getProxyObject(MUTTER_IDLE_SERVICE, MUTTER_IDLE_PATH);
      const iface = obj.getInterface(MUTTER_IDLE_INTERFACE);

      // dbus-next types interface methods via an index signature ([name: string]:
      // Function), so noUncheckedIndexedAccess flags direct calls as possibly
      // undefined even though these methods are always present on this
      // well-known interface. Route calls through this small helper instead of
      // sprinkling non-null assertions.
      const callMethod = <T>(name: string, ...args: unknown[]): Promise<T> => {
        const method = iface[name];
        if (!method) throw new Error(`${MUTTER_IDLE_INTERFACE} has no method ${name}`);
        return method.apply(iface, args) as Promise<T>;
      };

      // AddIdleWatch repeats every time the threshold is crossed, but
      // AddUserActiveWatch fires once and is then removed, so it has to be
      // re-armed after each idle period.
      const idleWatchId = await callMethod<number>('AddIdleWatch', thresholdMs);
      let activeWatchId: number | undefined;

      const armActiveWatch = async () => {
        activeWatchId = await callMethod<number>('AddUserActiveWatch');
        // Activity may have resumed while the call was in flight, in which
        // case the watch will never fire for it.
        const idleMs = await callMethod<bigint | number>('GetIdletime');
        if (Number(idleMs) < thresholdMs) {
          activeWatchId = undefined;
          if (!disposed) cb(false);
        }
      };

      iface.on('WatchFired', (id: number) => {
        if (disposed) return;
        if (id === idleWatchId) {
          cb(true);
          armActiveWatch().catch((err) => {
            console.error('[gnome-wayland] failed to arm user-active watch:', err);
          });
        } else if (id === activeWatchId) {
          activeWatchId = undefined;
          cb(false);
        }
      });
    })().catch((err) => {
      console.error('[gnome-wayland] failed to subscribe to IdleMonitor:', err);
    });

    return () => {
      disposed = true;
      bus.disconnect();
    };
  }
}
