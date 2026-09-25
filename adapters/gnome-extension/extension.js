import GLib from 'gi://GLib';
/**
 * Focus adapter for GNOME Shell (Wayland). Reports the currently focused
 * window over a D-Bus signal so the tracker daemon (apps/daemon) can turn
 * it into sessions. This extension does no tracking, storage, or network
 * access itself — it only reports what's already visible to the shell.
 *
 * See docs/architecture.md#focus-adapters and docs/adapters.md.
 */
import Gio from 'gi://Gio';
import Shell from 'gi://Shell';
import { Extension } from 'resource:///org/gnome/shell/extensions/extension.js';

const APP_ID = 'io.github.akkki007.screentime';
const FOCUS_INTERFACE_NAME = `${APP_ID}.Focus`;
const FOCUS_OBJECT_PATH = `/${APP_ID.replaceAll('.', '/')}/Focus`;

const FocusIface = `
<node>
  <interface name="${FOCUS_INTERFACE_NAME}">
    <signal name="FocusChanged">
      <arg type="s" name="appId"/>
      <arg type="s" name="title"/>
      <arg type="u" name="pid"/>
    </signal>
  </interface>
</node>`;

export default class ScreentimeFocusExtension extends Extension {
  enable() {
    this._dbusImpl = Gio.DBusExportedObject.wrapJSObject(FocusIface, this);
    this._dbusImpl.export(Gio.DBus.session, FOCUS_OBJECT_PATH);

    this._tracker = Shell.WindowTracker.get_default();
    this._focusHandlerId = global.display.connect('notify::focus-window', () => {
      this._emitFocusChanged();
    });

    // Report the initially focused window immediately.
    this._emitFocusChanged();
  }

  disable() {
    if (this._focusHandlerId) {
      global.display.disconnect(this._focusHandlerId);
      this._focusHandlerId = null;
    }
    this._dbusImpl?.unexport();
    this._dbusImpl = null;
    this._tracker = null;
  }

  _emitFocusChanged() {
    const win = global.display.get_focus_window();
    if (!win) return;

    const app = this._tracker.get_window_app(win);
    const appId = app?.get_id() ?? win.get_wm_class() ?? 'unknown';
    const title = win.get_title() ?? '';
    const pid = win.get_pid?.() ?? 0;

    this._dbusImpl.emit_signal('FocusChanged', new GLib.Variant('(ssu)', [appId, title, pid]));
  }
}
