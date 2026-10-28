# Adding a desktop adapter

The tracker daemon is desktop-agnostic. All desktop-specific focus/idle detection lives behind a single interface, `Provider`, defined in `internal/focus/focus.go`:

```go
type Provider interface {
	ID() string // "gnome-wayland"
	Available(ctx context.Context) bool
	OnFocusChange(cb func(Window)) (unsubscribe func())
	OnIdleChange(cb func(idle bool), thresholdMs int64) (unsubscribe func())
}
// Window{AppID, Title, PID, Ts}
```

The daemon picks an adapter at startup, in priority order, from a per-OS list: on Linux by `$XDG_SESSION_TYPE` and `$XDG_CURRENT_DESKTOP` (`cmd/screentimed/providers_linux.go`), on Windows and macOS the one provider for the OS (`providers_windows.go`, `providers_darwin.go`).

## Existing adapters

| Adapter | Focus source | Idle source | Status |
| --- | --- | --- | --- |
| `gnome-wayland` | Own GNOME Shell extension, sends `FocusChanged` over D-Bus to the daemon alone | `org.gnome.Mutter.IdleMonitor` idle watches | v1, tested on GNOME 50.1 |
| `x11` | `_NET_ACTIVE_WINDOW` over the X protocol (xgb) | MIT-SCREEN-SAVER (xgb) | v1.x, integration-tested against Xvfb in CI |
| `windows` | `SetWinEventHook` on `EVENT_SYSTEM_FOREGROUND` (Win32 through `x/sys/windows`, no cgo) | `GetLastInputInfo`, polled | Daemon only; builds and is vetted in CI, not yet run on Windows ([ADR 13](adr/0013-windows-and-macos-providers.md)) |
| `macos` | `NSWorkspaceDidActivateApplicationNotification` (Objective-C through cgo) | `CGEventSourceSecondsSinceLastEventType`, polled | Daemon only; built in CI, not yet run on a Mac ([ADR 13](adr/0013-windows-and-macos-providers.md)) |
| `kde-wayland` | Own KWin script, calls `FocusChanged` on the daemon over D-Bus | `ext-idle-notify-v1` (Wayland) | Implemented; not yet run on Plasma ([ADR 11](adr/0011-kde-wayland-adapter.md)) |
| `hyprland` / `sway` | Compositor IPC socket events | `ext-idle-notify-v1` (client already in `internal/focus/waylandidle.go`) | v2 |

## Notes on the existing adapters

**`gnome-wayland`.** The extension owns the bus name `io.github.akkki007.screentime` and exposes `Subscribe()`, `SetCaptureTitles()`, `GetFocus()` and a `FocusChanged` signal that it sends to the subscribed daemon alone (never broadcast; titles stay empty until the user opts in). GNOME deactivates extensions on the lock screen and loads them in parallel with login, so the daemon never depends on the extension being present at start: it watches the bus name, and each time the name appears it subscribes and calls `GetFocus` to catch up. Idle comes from real `org.gnome.Mutter.IdleMonitor` watches over the same persistent connection, so there is no polling and no lag. Windows without a `.desktop` file get a synthetic `window:<n>` ID from GNOME; the extension falls back to `WM_CLASS` and the daemon folds any that slip through into `unknown`.

**`kde-wayland`.** KWin scripts can call D-Bus methods but can't own a name or receive signals, so the direction is the reverse of GNOME's: the daemon owns `io.github.akkki007.screentime.Daemon` and the script (`adapters/kwin-script`) calls `FocusChanged(appId, title, pid)` on it, a method call to the daemon alone. The daemon refuses callers that aren't the owner of `org.kde.KWin`, and its reply tells the script whether titles are on, so they stay `''` until the user opts in. Idle uses the Wayland protocol `ext-idle-notify-v1` directly, because `org.freedesktop.ScreenSaver.GetSessionIdleTime` answers `NotSupported` on Wayland. The script can't tell when the daemon starts, so after a daemon restart the current window is picked up at the next switch.

**`windows`.** `internal/focus/windows.go`. A locked OS thread installs a WinEvent hook for `EVENT_SYSTEM_FOREGROUND` (out of context) and sleeps in `GetMessage`, so focus is event-driven. The app ID is the executable's base name without `.exe`, lowercased (`chrome`, `code`): it is stable across updates and needs no registry lookups, at the price that UWP apps all show as `applicationframehost`. Titles come from `GetWindowTextW` only after the user opts in. Idle polls `GetLastInputInfo` every 5 s (a quarter of the threshold, between 1 and 5 s) and reports only crossings; Windows offers no idle notification to a plain process. The daemon must run in the user's interactive session.

**`macos`.** `internal/focus/darwin.go`, cgo with a few lines of Objective-C in the preamble. It observes `NSWorkspaceDidActivateApplicationNotification`; the app ID is the bundle identifier, falling back to the localized name. Those notifications arrive on the main thread's run loop, so `cmd/screentimed` keeps its main goroutine on the main thread and runs that loop (`mainthread_darwin.go`). Titles are always empty: they need the Accessibility permission, which a background daemon can't request. Idle polls `CGEventSourceSecondsSinceLastEventType` like Windows. A `CGO_ENABLED=0` build compiles but reports the provider unavailable.

**`x11`.** Speaks the X protocol directly (`github.com/jezek/xgb`): focus from the root window's `_NET_ACTIVE_WINDOW`, idle from the MIT-SCREEN-SAVER extension, so neither `xprop` nor `xprintidle` is needed. `WM_CLASS` is resolved to the desktop-entry ID through `StartupWMClass`, so an app has the same ID as under Wayland. This is the adapter used for GNOME on Xorg too.

## Adding a new one

1. Create a new directory under `adapters/` for anything that must run outside the daemon process (a Shell extension, a compositor script, etc.), or a new file under `internal/focus/` if the adapter can talk to the OS directly from the daemon (a D-Bus interface that already exists, an X protocol connection, a platform API behind a build tag).
2. Implement `focus.Provider`. `Available()` should check session type/desktop and any required extension, returning `false` rather than failing when the adapter doesn't apply.
3. Add the adapter to the candidate list in `cmd/screentimed/providers_<os>.go` (`focusCandidates`), in priority order for its session type, and give its file the matching build tag (`//go:build linux`) so other OSes don't link it. `SCREENTIME_FOCUS_PROVIDER=<id>` forces one.
4. Add app-identity mapping: `appId` should resolve to the `.desktop` file ID (e.g. `org.mozilla.firefox`), falling back to `WM_CLASS` when no `.desktop` file matches.
5. Respect the window-title opt-in — only populate `title` when the user has explicitly enabled it in settings.
6. Add a short section to this doc's adapter table above.

## Testing an adapter

Since adapters depend on a live desktop session, they're best tested manually. The OS-independent parts (app-ID mapping, idle crossings, path selection) have unit tests that run everywhere:

```bash
SCREENTIME_DEBUG=1 go run ./cmd/screentimed
# focus different windows / go idle, and watch the daemon log focus and idle events
```

Automated tests should not need a real session either. The providers' integration tests run in CI against a private `dbus-daemon` with a fake extension (`internal/focus/gnome_test.go`) and a private Xvfb server (`internal/focus/x11_test.go`); the clock is injected (`Now`) so idle and suspend cases stay deterministic. Keep parsing in small pure functions so it is testable on its own.
