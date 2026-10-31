# Screentime for GNOME Shell

A GJS (ESM, GNOME 45+) Shell extension, shown as **Screentime** in the Extensions app. It does two things:

- **The focus adapter.** It reports the focused window over D-Bus. This is the v1 adapter — the only reliable focus source on GNOME Wayland — consumed by the daemon's `gnome-wayland` provider (`internal/focus/gnome.go`). It does no tracking, storage or network access.
- **The island** at the top centre of the screen ([ADR 14](../../docs/adr/0014-top-of-screen-notch.md)), when the tracker's `notchEnabled` setting is on (off by default; turn it on in the app's Settings, during onboarding, or in this extension's settings).

## The island

| File | What it does |
| --- | --- |
| `notchUI.js` | The island: one shape whose width, height and corner radius follow springs, with the closed island, alerts and the open panel cross-fading inside it |
| `notchPanel.js` | The open Screen Time panel, drawn in the user's theme |
| `notch.js` | What the closed island says (shared fixtures with `internal/notch`) |
| `spring.js` | Retargetable damped springs (`playful` overshoots a little, `calm` does not) |
| `palette.js` | Styles, accents and sizes, shared by the Shell and the settings window |
| `prefs.js`, `schemas/` | The settings window (gear in the Extensions app, or in the open island) and its GSettings schema |
| `daemonClient.js` | The JSON-RPC client for the tracker's socket |

Rest the pointer on it to open it (or click, if set so); it closes 300 ms after the pointer leaves, or with Escape or a click elsewhere when it was opened by a click. Resting on an alert holds it; a right-click puts it away. If the tracker stops while the island is in use, it tries once to start `screentime-daemon.service`, then shows "Screentime is not running" (click it to try again).

**Settings** (all live, no restart): style (match the top bar, black, graphite, light), accent colour, size, placement (attached to the bar or floating below it), what the closed island shows on the left, in the middle and on the right, open on hover or click and the hover delay, motion (playful, calm, off; the system's reduce-motion setting always wins), display (main or the one in use), keep windows clear of the island, alerts in the island, the greeting and what to call you.

The schema must be compiled next to it: `install.sh` and the `.deb` build run `glib-compile-schemas` on `schemas/` (`gschemas.compiled` is git-ignored).

`metadata.json` declares GNOME 45–50. It has only been run on GNOME 50.1; please test the others before relying on that range.

## Install (development)

```bash
adapters/gnome-extension/install.sh              # symlink into ~/.local/share/gnome-shell/extensions and try to enable
adapters/gnome-extension/install.sh --uninstall
```

On Wayland, **log out and back in** once so GNOME Shell discovers a newly installed extension (and again after editing `extension.js`, which Shell caches), then `gnome-extensions enable screentime-focus@akkki007.github.io`. On X11, `Alt+F2`, `r`, Enter.

Installed from the `.deb`, it lives in `/usr/share/gnome-shell/extensions/` and only needs enabling.

GNOME marks the extension *inactive* while the screen is locked. That is expected; the daemon reconnects when it comes back.

## D-Bus contract

Owns the name `io.github.akkki007.screentime` and exports `/io/github/akkki007/screentime/Focus` with interface `io.github.akkki007.screentime.Focus`:

- method `Subscribe()`: the caller becomes the one client that receives `FocusChanged`. A second client gets `org.freedesktop.DBus.Error.AccessDenied` until the first leaves the bus.
- method `SetCaptureTitles(b capture)`: subscriber only. Titles are sent as `''` until it is `true`; the daemon passes the user's "Record window titles" setting.
- signal `FocusChanged(s appId, s title, u pid)`: sent to the subscriber alone (a unicast signal), whenever the focused window changes.
- method `GetFocus() -> (s appId, s title, u pid)`: the current window, for a daemon that starts later. Anyone but the subscriber gets `('', '', 0)`.

`appId` is the `.desktop` file ID, falling back to `WM_CLASS` for windows with no desktop entry.

### Privacy

On Wayland, apps can't normally see each other's window titles, and titles often hold document names, chat contacts and page titles. So nothing goes on the bus for other processes to read, and titles don't leave the Shell unless the user opted in (issue #3; `docs/migration-to-go.md`, S6).

Until a daemon subscribes, the extension sends nothing and `GetFocus` answers `('', '', 0)`; it never falls back to a broadcast.

## Manual test (privacy, issue #3)

The Go daemon's side is tested in CI against a fake extension. Run the extension itself through this in a real session before closing #3:

1. Install it with `./install.sh`, log out and back in, then `gnome-extensions enable screentime-focus@akkki007.github.io`.
2. With no daemon running, `gdbus monitor --session --dest io.github.akkki007.screentime` shows **nothing** when you switch windows (there is no broadcast mode any more).
3. Stop the service if it is running (`systemctl --user stop screentime-daemon`). Then from the repo root run:
   ```bash
   go build -o bin/screentimed ./cmd/screentimed && SCREENTIME_DEBUG=1 bin/screentimed
   ```
4. Switching windows logs `focus -> <app>` lines without titles.
5. In another terminal, `gdbus monitor --session --dest io.github.akkki007.screentime` now shows **nothing** when you switch windows.
6. From another terminal, these calls must be refused:

   | Call | Expected result |
   | --- | --- |
   | `GetFocus` | `('', '', uint32 0)` |
   | `Subscribe` | `AccessDenied` |
   | `SetCaptureTitles true` | `AccessDenied` |

   For example:
   ```bash
   gdbus call --session --dest io.github.akkki007.screentime \
     --object-path /io/github/akkki007/screentime/Focus \
     --method io.github.akkki007.screentime.Focus.GetFocus
   ```
7. Turn on "Record window titles". Titles appear in the daemon log, and still nothing in `gdbus monitor`.
8. Lock and unlock. The daemon re-subscribes, and `gdbus monitor` still shows nothing.
