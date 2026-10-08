# 9. The UI shell is Wails v3, with windows created on demand

Date: 2026-10-08

## Status

Accepted. Supersedes [ADR 2](0002-electrobun-with-a-bun-main-process.md) (Electrobun with a Bun main process) and updates [ADR 3](0003-ui-reaches-the-daemon-through-its-main-process.md) (the main process is now Go).

## Context

[ADR 7](0007-migrate-to-go.md) moves every process we run to Go, which leaves the UI's main process: Electrobun's Bun process had to go, and the migration notes named Wails as the replacement and left "v2 or v3" open until step 6. The shell needs a system tray, a small frameless quick panel next to the dashboard, a minimum window size, and a way to keep the app alive with every window closed.

| | Wails v2 (2.16) | Wails v3 (3.0.0-beta.28) |
| --- | --- | --- |
| Release status | Stable | Beta, API still moving |
| Windows | One | Any number; a window can be attached to the tray |
| System tray | None; needs a library that talks to D-Bus on its own | Built in (StatusNotifierItem on Linux), with menu and attached window |
| Linux webview | WebKitGTK 4.0/4.1 | WebKitGTK 4.1 with `-tags gtk3` (the default is GTK 4 and WebKitGTK 6) |
| Dev server | Built in | Built in, development builds only |

The quick panel is a second window and the tray is core UX, so v2 would have meant either a second process for the panel or dropping it.

## Decision

Use **Wails v3, pinned to `3.0.0-beta.28`**, built with `-tags gtk3` so the package keeps depending on `libwebkit2gtk-4.1-0` and `libgtk-3-0` exactly like the Electrobun build did (Ubuntu 22.04 and later). The pin goes in `go.mod` and `frontend/package.json` (`@wailsio/runtime`); upgrading is a deliberate change, tested on a real GNOME session.

- `cmd/screentime` is thin glue. Everything testable lives in `internal/shell`: the reconnecting daemon client (built on `rpc.Dial`, which enforces S3), the tray menu model, safe export writing (S5) and the bridge.
- The webview reaches the daemon through one bound Go service, `Bridge`, with a `Daemon(method, params)` call that forwards only an explicit list of the UI's methods (ADR 3 stands). Daemon notifications arrive as `daemon:event` and `daemon:connection` events.
- **S4:** the UI is served from the build embedded in the binary. Wails' `FRONTEND_DEVSERVER_URL` override exists only in builds without `-tags production`; the `.deb` and CI build with it, so a packaged app cannot be pointed at a dev server, and a development build needs the variable set on purpose.
- **Windows are created when opened and destroyed when closed.** The tray keeps the process alive (`DisableQuitOnLastWindowClosed`), so an idle app holds no webview. Closing the dashboard hides to the tray only if a StatusNotifier watcher exists; without one (stock GNOME without the AppIndicator extension) closing quits the UI, so the user is never left with an app they can't reach. A second launch raises the running window (single instance).
- The Svelte UI moved to `frontend/` unchanged apart from its bridge; the in-memory mock bridge for browser previews and screenshots stays.

## Consequences

- No Bun and no Electrobun toolchain (Hutch) in the build or the package. The `.deb` fell from ~30 MB (86 MB installed) to 6.7 MB (19 MB installed), and `bun run typecheck` no longer downloads anything.
- We depend on a beta. The mitigation is the pin, the thin `cmd/screentime`, and that the shell is a client of the daemon: if v3 stalls, the same `internal/shell` and `frontend/` run under v2 plus a tray library, or under another shell.
- Memory while a window is open is dominated by WebKitGTK itself: a bare Wails window measured ~290 MB PSS in the sandbox, the dashboard ~500 MB (software rendering, so a ceiling). Creating windows on demand is what keeps the resident cost at the tray's ~75 MB. The tracker, which is always on, stays at ~10 MB.
- Wayland compositors ignore requested window positions, so the quick panel appears where GNOME puts it, as it did under Electrobun.
- Verified in a sandbox only: the app against a real daemon under Xvfb with a window manager, a fake StatusNotifier watcher (the tray item registers), the dashboard, the quick panel, export, minimum size, single instance, and closing to the tray. Not verified on a real GNOME session: tray clicks, the panel's position and transparency, and the AppIndicator behaviour.
