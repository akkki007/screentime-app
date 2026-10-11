# 11. KDE Wayland adapter: a KWin script that calls the daemon, and idle from ext-idle-notify-v1

Date: 2026-10-10

## Status

Proposed. Not yet run on a real Plasma session; see "Left to check" below. Part of step 7 of the Go migration (#28).

## Context

On KDE Plasma under Wayland, as on GNOME, no ordinary client can see which window is focused: only the compositor (KWin) knows. The roadmap planned "a KWin script that calls the daemon over D-Bus" for focus and `org.freedesktop.ScreenSaver` for idle.

Two constraints decide the shape:

- **KWin scripts can call D-Bus methods (`callDBus`) but cannot own a bus name, export an object or receive signals.** So the GNOME design, where the extension owns a name and the daemon subscribes (S6), cannot be copied. The call has to go from the script to the daemon.
- **`org.freedesktop.ScreenSaver.GetSessionIdleTime` is not available on Wayland.** kscreenlocker answers it with `NotSupported` when `KWindowSystem::isPlatformWayland()`. Plasma has no other idle D-Bus API.

## Decision

**Focus.** The daemon's `kde-wayland` provider (`internal/focus/kde.go`) owns `io.github.akkki007.screentime.Daemon` and exports `io.github.akkki007.screentime.KWin.FocusChanged(s appId, s title, s pid) -> b captureTitles` at `/io/github/akkki007/screentime/KWin`. The KWin script (`adapters/kwin-script`) calls it on each window activation.

- It is a method call to one destination, so nothing is broadcast to the session bus (S6, #3).
- The daemon accepts the call only from the current owner of `org.kde.KWin`, which is the connection `callDBus` uses, and answers anyone else with `AccessDenied`. This is the same check the GNOME provider makes on `FocusChanged`.
- The reply carries the user's "Record window titles" setting, and the script sends `''` as the title until a reply says `true`. The daemon drops titles as well when the setting is off.
- `pid` is a string, because `callDBus` turns JavaScript numbers into doubles.
- godbus runs each incoming call on its own goroutine, so two quick switches can arrive out of order. The provider delivers calls one at a time and drops any whose message serial is older than the last one it delivered, because a newer window already has the focus.
- The app ID is KWin's `desktopFileName` (the `.desktop` file ID GNOME reports, so an app has the same ID on both), with `resourceClass` as the fallback.

**Idle.** The provider speaks the Wayland protocol `ext-idle-notify-v1` itself (`internal/focus/waylandidle.go`): under 300 lines for the few messages it needs, with no Wayland library and no cgo. The compositor sends `idled` after the threshold without input, and `resumed` on the next input. No polling. `get_idle_notification` honours idle inhibitors, so a video that keeps the screen on is not counted as time away. That is what a screentime app wants, and it differs from GNOME's Mutter watches, which count input only.

KWin, sway, Hyprland and other wlroots compositors all implement `ext-idle-notify-v1`, so the wlroots adapters can reuse the idle half as it is.

## Consequences

- The daemon now owns a well-known session-bus name on KDE. A second daemon gets no name and logs that focus is not tracked, much as it already refuses to start on a live socket.
- **No catch-up after the daemon starts.** The script cannot learn that the daemon appeared, and the daemon cannot ask a KWin script anything, so the window focused when the daemon (re)starts is picked up at the next window switch. The script reports the active window when it loads, which covers a login where KWin starts after the daemon, and `install.sh` reloads it.
- **Same-user processes are not a boundary.** A process that takes `…screentime.Daemon` while no daemon runs would receive app IDs (and could reply `true` to get titles). This is the same exposure as the GNOME design, where the first subscriber wins, and any same-user process can already monitor the session bus.
- The daemon reads `$WAYLAND_DISPLAY` and `$XDG_RUNTIME_DIR` for idle. Plasma exports them to the systemd user manager at login. Without them it tries `wayland-0`, and if that fails it logs that time away will be counted, as the X11 provider does without MIT-SCREEN-SAVER.

## Verification

- `internal/focus/kde_test.go`: a fake KWin on a private `dbus-daemon`. Titles stay out until opt-in, callers other than KWin get `AccessDenied` (also when no KWin is on the bus), a call overtaken by a newer one is dropped, and unsubscribing releases the name.
- `internal/focus/waylandidle_test.go`: a fake compositor over a Unix socket. Each crossing is reported once, nothing after stop, and an error when the protocol is missing.
- The idle client was run against a real compositor, headless sway 1.9. `idled` came 1.5 s after the start with a 1.5 s threshold, `resumed` on input, and `idled` again 1.5 s later.

## Left to check on a Plasma session

Plasma 6 on Wayland, and Plasma 5 if it is still worth supporting:

1. `adapters/kwin-script/install.sh`, then `SCREENTIME_DEBUG=1 screentimed`. Switch windows; each switch is logged with the right app ID (Konsole is `org.kde.konsole`, Firefox `firefox` or `org.mozilla.firefox`).
2. With titles off, watch the bus with `dbus-monitor "interface='io.github.akkki007.screentime.KWin'"` and check that every title is `''`. Turn titles on and check they appear from the second switch.
3. Leave the machine for longer than the idle threshold. The session ends, and starts again on input. Play a full-screen video for longer than the threshold; it should stay counted.
4. Restart the daemon (`systemctl --user restart screentime-daemon`) and check that tracking resumes at the next window switch.
5. Lock and unlock the screen.
