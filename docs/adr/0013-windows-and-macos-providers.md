# 13. Windows and macOS focus providers for the daemon

Date: 2026-10-28

## Status

Accepted. Neither provider has been run on real Windows or macOS hardware yet; see "Not done" and "Left to check". Part of step 7 of the Go migration (#28).

## Context

`screentimed` was Linux-only: every provider spoke D-Bus, X11 or Wayland, and the socket code used Unix uids and modes. The product's edge is a small, event-driven daemon, so the ports must not make the Linux binary heavier, add third-party dependencies or poll where the OS can notify.

## Decision

**Windows** (`internal/focus/windows.go`, ID `windows`, no cgo, `golang.org/x/sys/windows`).

- Focus: `SetWinEventHook(EVENT_SYSTEM_FOREGROUND, WINEVENT_OUTOFCONTEXT)` on a goroutine locked to its OS thread, which sits in `GetMessage`. Hook callbacks are delivered through that loop, so there is no polling. Unsubscribing posts `WM_QUIT`, and the thread unhooks itself (it must be the installing thread).
- App ID: the executable's base name without `.exe`, lowercased (`chrome`), from `GetWindowThreadProcessId` and `QueryFullProcessImageNameW`. It survives updates and install-path changes, and is readable as a fallback name. Costs: two products with one exe name share an ID, and UWP apps all report `applicationframehost`; a process we can't open is `unknown`.
- Titles: `GetWindowTextW`, only after `SetCaptureTitles(true)` (privacy default off, ADR 5).
- Idle: `GetLastInputInfo` against `GetTickCount64`, compared on the low 32 bits so the 49.7-day wrap is harmless. Windows gives a plain process no idle notification, so this polls: every quarter of the threshold, clamped to 1-5 s (always 5 s for the UI's whole-minute thresholds), reporting only crossings. That is one cheap call per 5 s, the same cost as the X11 provider's poll.

**macOS** (`internal/focus/darwin.go`, ID `macos`, cgo with Objective-C in the preamble).

- Focus: an observer for `NSWorkspaceDidActivateApplicationNotification` on `[[NSWorkspace sharedWorkspace] notificationCenter]`, plus the frontmost app at subscription. App ID is the bundle identifier, falling back to the localized name; the PID is `processIdentifier`.
- The notifications are posted on the main thread, so the program has to run the main thread's run loop. `cmd/screentimed` locks the main goroutine to the main thread in an `init` and runs `CFRunLoopRun` there while the daemon runs on another goroutine (`mainthread_darwin.go`). A provider that locks its own thread would be simpler, but would receive nothing.
- Titles are always empty and the provider does not implement `TitleCapture`: window titles need the Accessibility permission, which a background daemon can't request without a UI.
- Idle: `CGEventSourceSecondsSinceLastEventType(kCGEventSourceStateCombinedSessionState, kCGAnyInputEventType)`, polled like Windows.
- A `CGO_ENABLED=0` build compiles (a stub reports itself unavailable), so `go vet` works without Xcode.

**Portability of the rest.**

- Per-OS candidate lists, umask and notifier live in `cmd/screentimed/providers_{linux,windows,darwin}.go` and `process_*.go`. `main.go` calls `focusCandidates`, `appDirs`, `newNotifier` and `restrictUmask`; a new Linux provider is one more line in `providers_linux.go`.
- The D-Bus, X11 and Wayland providers and the D-Bus notifier are `//go:build linux`, so they are not linked into the other binaries.
- Data: `%LOCALAPPDATA%\screentime\screentime.db` on Windows, `~/Library/Application Support/screentime/screentime.db` on macOS. Linux is unchanged (`$XDG_DATA_HOME`, then `~/.local/share`).
- Socket (AF_UNIX works on Windows 10 1803+ and on macOS): `%LOCALAPPDATA%\screentime\daemon.sock` and `~/Library/Caches/screentime/daemon.sock`. macOS uses a fixed home-relative path rather than `$TMPDIR`, because the daemon, a terminal and a browser's native-messaging launch can see different `$TMPDIR`s. Linux is unchanged (S2 byte for byte).
- macOS keeps the Unix S2/S3 checks (owner, 0700, 0600 socket). **Windows has no uid, mode bits or umask**, so those checks are replaced, not skipped: the directory and the socket must be owned by the current user's SID (`GetNamedSecurityInfo`), the directory must not be a symlink or junction, and what keeps other users out is the inherited ACL of `%LOCALAPPDATA%`, which admits only the user, SYSTEM and administrators. Windows has no `SO_PEERCRED`, so the check that the peer process runs as the same user is absent there; the owner check on the socket file stands in for it. A daemon started from an elevated shell creates files owned by the Administrators group and would fail the owner check; it is meant to run as the logged-in user.
- Notifications: `internal/notify` has the D-Bus notifier on Linux and `notify.Log` elsewhere, which writes the notification to the daemon log. No toast library is added.
- App names and icons come from `.desktop` files, so on Windows and macOS an app shows as its app ID until a display-name source exists.

## Consequences

- The Linux binary links the same packages as before; the Windows binary has no D-Bus or X11 code.
- CI builds the daemon and runs the focus, rpc and store tests plus the daemon's on `windows-latest` and `macos-latest`. The darwin cgo file is compiled there, and nowhere else.
- Pure logic shared by the ports (exe-name to app ID, bundle-ID fallback, tick-wrap arithmetic, idle crossings, poll interval, per-OS data and socket paths) is unit-tested on Linux too.

## Not done

- No installer, no packaging, no autostart (a Run key or LaunchAgent). The daemon is started by hand.
- No notifications on either OS: limits and break reminders are enforced and logged, not shown.
- No UI shell: `cmd/screentime` (Wails, GTK) is Linux-only, so there is no dashboard, tray or settings on these OSes yet. Reaching the daemon needs a client of the socket.
- No native-messaging manifest installer for browsers on these OSes (the host itself compiles).
- No display names or icons, and no UWP/AppUserModelID or per-window (rather than per-app) handling on Windows.
- Windows service-session and fast-user-switching behaviour, and macOS login-window and screen-lock behaviour, are unexamined.

## Left to check

On real machines: that app switches are logged with the expected IDs (`SCREENTIME_DEBUG=1`); that idle starts and ends around the threshold; on macOS, that notifications keep arriving (the run loop is the likeliest thing to be wrong) and that the daemon needs no permission prompt; on Windows, that `Dial` passes the owner check for a normal (non-elevated) user, including on a domain-joined or Microsoft-account profile where the owner SID may differ from the token user.
