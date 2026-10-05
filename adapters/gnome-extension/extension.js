/**
 * Focus adapter for GNOME Shell (Wayland). Reports the focused window to the
 * tracker daemon over D-Bus so it can turn it into sessions. This extension
 * does no tracking, storage, or network access itself.
 *
 * Privacy (issue #3, docs/migration-to-go.md S6): window titles often hold
 * private details, and on Wayland other apps can't normally see them. So:
 *   - The daemon calls Subscribe(). Only one client may be subscribed;
 *     FocusChanged then goes to that client alone, never broadcast.
 *   - Titles are sent as '' until the subscriber turns them on with
 *     SetCaptureTitles(true) (the user's "Record window titles" setting).
 *   - GetFocus answers anyone else with nothing.
 *
 * Legacy mode: the Bun daemon reaches D-Bus through `gdbus monitor`, which
 * can't subscribe, so until some client subscribes in this Shell session the
 * old behaviour (a broadcast signal, with titles) is kept. It goes away with
 * the Bun daemon (migration step 5).
 *
 * See docs/architecture.md#focus-adapters and docs/adapters.md.
 */
import GLib from 'gi://GLib';
import Gio from 'gi://Gio';
import Shell from 'gi://Shell';
import { Extension } from 'resource:///org/gnome/shell/extensions/extension.js';

const APP_ID = 'io.github.akkki007.screentime';
const FOCUS_INTERFACE_NAME = `${APP_ID}.Focus`;
const FOCUS_OBJECT_PATH = `/${APP_ID.replaceAll('.', '/')}/Focus`;
const ACCESS_DENIED = 'org.freedesktop.DBus.Error.AccessDenied';

const FocusIface = `
<node>
  <interface name="${FOCUS_INTERFACE_NAME}">
    <method name="GetFocus">
      <arg type="s" name="appId" direction="out"/>
      <arg type="s" name="title" direction="out"/>
      <arg type="u" name="pid" direction="out"/>
    </method>
    <method name="Subscribe"/>
    <method name="SetCaptureTitles">
      <arg type="b" name="capture" direction="in"/>
    </method>
    <signal name="FocusChanged">
      <arg type="s" name="appId"/>
      <arg type="s" name="title"/>
      <arg type="u" name="pid"/>
    </signal>
  </interface>
</node>`;

// Module scope outlives disable/enable (GNOME disables extensions on the
// lock screen), so once a daemon has subscribed, unlocking never falls back
// to legacy broadcasts.
let everSubscribed = false;

export default class ScreentimeFocusExtension extends Extension {
  enable() {
    this._subscriber = null;
    this._subscriberWatch = 0;
    this._captureTitles = false;

    this._dbusImpl = Gio.DBusExportedObject.wrapJSObject(FocusIface, this);
    this._dbusImpl.export(Gio.DBus.session, FOCUS_OBJECT_PATH);
    // The daemon finds the object by bus name, so the name must be owned.
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
    // No signal for the window focused right now: a daemon asks with GetFocus
    // when the name appears.
  }

  disable() {
    if (this._focusHandlerId) {
      global.display.disconnect(this._focusHandlerId);
      this._focusHandlerId = null;
    }
    this._unsubscribe();
    this._dbusImpl?.unexport();
    this._dbusImpl = null;
    if (this._nameOwnerId) {
      Gio.bus_unown_name(this._nameOwnerId);
      this._nameOwnerId = null;
    }
    this._tracker = null;
  }

  /** D-Bus: become the one client that receives FocusChanged. */
  SubscribeAsync(_params, invocation) {
    const sender = invocation.get_sender();
    if (this._subscriber && this._subscriber !== sender) {
      invocation.return_dbus_error(ACCESS_DENIED, 'another client is already subscribed');
      return;
    }
    if (!this._subscriber) {
      this._subscriber = sender;
      this._captureTitles = false;
      everSubscribed = true;
      // When the daemon exits (or restarts), let the next one subscribe.
      this._subscriberWatch = Gio.bus_watch_name_on_connection(
        Gio.DBus.session,
        sender,
        Gio.BusNameWatcherFlags.NONE,
        null,
        () => this._unsubscribe(),
      );
    }
    invocation.return_value(null);
  }

  /** D-Bus: the subscriber's "Record window titles" setting. */
  SetCaptureTitlesAsync([capture], invocation) {
    if (invocation.get_sender() !== this._subscriber) {
      invocation.return_dbus_error(ACCESS_DENIED, 'only the subscribed client may change this');
      return;
    }
    this._captureTitles = capture;
    invocation.return_value(null);
  }

  /** D-Bus: the window focused right now, for a daemon that starts after us. */
  GetFocusAsync(_params, invocation) {
    const focus = this._describeFocus();
    const sender = invocation.get_sender();
    let reply = ['', '', 0];
    if (focus && sender === this._subscriber) reply = this._forSubscriber(focus);
    else if (focus && !everSubscribed) reply = focus; // legacy mode
    invocation.return_value(new GLib.Variant('(ssu)', reply));
  }

  _unsubscribe() {
    if (this._subscriberWatch) {
      Gio.bus_unwatch_name(this._subscriberWatch);
      this._subscriberWatch = 0;
    }
    this._subscriber = null;
    this._captureTitles = false;
  }

  _forSubscriber([appId, title, pid]) {
    return [appId, this._captureTitles ? title : '', pid];
  }

  _describeFocus() {
    const win = global.display.get_focus_window();
    if (!win) return null;

    const app = this._tracker.get_window_app(win);
    // Windows without a .desktop file get a synthetic "window:<n>" ID that is
    // different every time; identify those by WM_CLASS instead.
    let appId = app?.get_id() ?? '';
    if (!appId || appId.startsWith('window:')) appId = win.get_wm_class() ?? 'unknown';
    const title = win.get_title() ?? '';
    const pid = win.get_pid?.() ?? 0;
    return [appId, title, pid];
  }

  _emitFocusChanged() {
    const focus = this._describeFocus();
    if (!focus) return;
    if (this._subscriber) {
      // Addressed to the subscriber only: nobody else on the bus receives it.
      Gio.DBus.session.emit_signal(
        this._subscriber,
        FOCUS_OBJECT_PATH,
        FOCUS_INTERFACE_NAME,
        'FocusChanged',
        new GLib.Variant('(ssu)', this._forSubscriber(focus)),
      );
    } else if (!everSubscribed) {
      this._dbusImpl.emit_signal('FocusChanged', new GLib.Variant('(ssu)', focus));
    }
  }
}
