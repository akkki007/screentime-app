/**
 * Focus adapter for KDE Plasma (KWin, Wayland). Reports the activated window
 * to the tracker daemon, which turns it into sessions. This script does no
 * tracking, storage or network access itself.
 *
 * KWin scripts can call D-Bus methods but not export objects or receive
 * signals, so the script calls the daemon (docs/adr/0011-kde-wayland-adapter.md):
 *   - FocusChanged is a method call to the daemon's name alone, never a
 *     broadcast, and the daemon refuses callers other than KWin.
 *   - Titles are sent as '' until the daemon's reply says the user turned on
 *     "Record window titles" (issue #3, docs/migration-to-go.md S6).
 */
const SERVICE = 'io.github.akkki007.screentime.Daemon';
const PATH = '/io/github/akkki007/screentime/KWin';
const INTERFACE = 'io.github.akkki007.screentime.KWin';

let captureTitles = false;

function appIdOf(window) {
  // The .desktop file ID (without .desktop), as GNOME reports; the window
  // class for apps that have none.
  let id = String(window.desktopFileName || '');
  if (id.endsWith('.desktop')) id = id.slice(0, -'.desktop'.length);
  return id || String(window.resourceClass || '') || 'unknown';
}

function report(window) {
  // null when focus leaves every window (e.g. the desktop is shown).
  if (!window) return;
  const title = captureTitles ? String(window.caption || '') : '';
  // pid as a string: a JavaScript number would reach D-Bus as a double.
  callDBus(
    SERVICE,
    PATH,
    INTERFACE,
    'FocusChanged',
    appIdOf(window),
    title,
    String(window.pid || 0),
    (capture) => {
      captureTitles = capture === true;
    },
  );
}

// Plasma 6 names it windowActivated; Plasma 5 clientActivated.
(workspace.windowActivated || workspace.clientActivated).connect(report);
report(workspace.activeWindow || workspace.activeClient);
