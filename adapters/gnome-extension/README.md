# GNOME Shell focus adapter

A small GJS (ESM, GNOME 45+) Shell extension that reports the focused window over D-Bus. This is the v1 adapter — the only reliable focus source on GNOME Wayland — consumed by the daemon's `gnome-wayland` provider (`internal/focus/gnome.go`).

It intentionally does nothing else: no tracking, no storage, no network access. Keeping it tiny is a deliberate mitigation against GNOME Shell API changes breaking it on upgrade (see `docs/architecture.md#risks-and-open-questions`).

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
