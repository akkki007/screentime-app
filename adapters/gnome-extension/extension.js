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
    <method name="GetFocus">
      <arg type="s" name="appId" direction="out"/>
      <arg type="s" name="title" direction="out"/>
      <arg type="u" name="pid" direction="out"/>
    </method>
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
    // The daemon resolves the object by bus name, so the name must be owned.
    this._nameOwnerId = Gio.bus_own_name_on_connection(
      Gio.DBus.session,
      APP_ID,
      Gio.BusNameOwnerFlags.NONE,
      null,
      null,
    );

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
    if (this._nameOwnerId) {
      Gio.bus_unown_name(this._nameOwnerId);
      this._nameOwnerId = null;
    }
    this._tracker = null;
  }

  /** D-Bus method: lets the daemon learn the current focus when it starts after us. */
  GetFocus() {
    return this._describeFocus() ?? ['', '', 0];
  }

  _describeFocus() {
    const win = global.display.get_focus_window();
    if (!win) return null;

    const app = this._tracker.get_window_app(win);
    const appId = app?.get_id() ?? win.get_wm_class() ?? 'unknown';
    const title = win.get_title() ?? '';
    const pid = win.get_pid?.() ?? 0;
    return [appId, title, pid];
  }

  _emitFocusChanged() {
    const focus = this._describeFocus();
    if (!focus) return;
    this._dbusImpl.emit_signal('FocusChanged', new GLib.Variant('(ssu)', focus));
  }
}
