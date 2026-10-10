# 10. Each window runs as its own process

Date: 2026-10-10

## Status

Accepted. Refines [ADR 9](0009-wails-v3-ui-shell.md), whose "windows are created when opened and destroyed when closed" turned out not to give the memory back.

## Context

ADR 9 kept the tray and the windows in one process and created windows on demand, expecting an idle tray to hold no webview and so to cost about what the tray costs. The first measurements were under Xvfb, where that looked true. On a real GNOME/Wayland session (Ubuntu 26.04, WebKitGTK 2.52.6, Mesa and NVIDIA drivers) it is not:

| State | PSS, whole app |
| --- | --- |
| Tray only, never opened a window | 39 MB |
| Dashboard open | 268 MB |
| Dashboard opened, then closed | **126 MB** |

The window's own processes (web and network) go away, but the tray process keeps ~90 MB it gained when the first window opened: the GL driver stack (`libLLVM`, the NVIDIA GL libraries) and WebKit's UI-side code, all `dlopen`ed and never unloaded. Turning off GPU compositing or DMA-BUF, or software rendering, changed none of it. A tray app is in this state nearly all day, so it is the number that decides whether we can call the app lightweight.

## Decision

Split the app into two kinds of process, from the one `screentime` binary:

- **The tray process** holds the tray icon, its menu and the connection to the daemon for status. It never creates a webview, so it never loads the GL or WebKit UI stack.
- **Each window is its own process**, `screentime -window main|panel`, started by the tray and gone when the window closes. Each has its own daemon connection and bridge, so the webview still never touches the socket (ADR 3, S3, S4, S5 are unchanged).

Details:

- A window process gets its own single-instance name (`…screentime.Dashboard`, `…screentime.Panel`), so a second launch raises the dashboard instead of opening another. The tray's own name is the app id, as before, so `screentime` with no arguments, from the launcher or a window process, opens the dashboard through the running tray.
- The tray toggles the panel by signalling it. A click on the tray icon is what takes focus from the panel, which then closes itself; a toggle within 400 ms of that is read as the same click and ignored, so one click no longer closes and reopens the panel.
- A window must not outlive its tray, including when the tray is killed. Each window process watches a pipe from the tray and quits when it closes. (`Pdeathsig` would be simpler but Go ties it to the OS thread that forked.)
- "Exit Screentime" in a window signals the tray, which closes its windows and quits.
- Without a StatusNotifier host (stock GNOME), closing the dashboard still quits the UI, as ADR 9 required: the tray process exits when the dashboard's does.
- JavaScriptCore's JIT is off in window processes (`JSC_useJIT=false`, `SCREENTIME_JIT=1` turns it back on). It saves ~13 MB in the web process and a dashboard of ~220 KB of script does not use it.
- The quick panel is placed at the top right of the primary monitor's work area. Wails could position it from the tray click only inside the tray process; on GNOME/Wayland the compositor ignores positions either way.
- The panel closes on Escape.

## Consequences

Measured on the same machine, production build (`-tags gtk3,production`, stripped):

| State | Before | After |
| --- | --- | --- |
| Tray only | 39 MB | 37 MB |
| Dashboard open | 268 MB | ~268 MB (tray 27 + window 95 + web 126 + network 15) |
| Dashboard closed | 126 MB | **37 MB** |

- The steady state, a tray with no window, is 37 MB and the daemon is ~10 MB. Together that is under 50 MB for the always-on part.
- An open window costs what WebKitGTK costs. We have nothing left to shave there that is not the page itself, and the page is already plain (no blur, backdrop filters or animations; ~220 KB of script, one variable font).
- Opening a window starts a process: 0.3 s from the click to a mapped dashboard on the test machine, about what creating a window in-process cost, since the web process is spawned either way.
- A bug in a window process cannot take the tray, and with it the tray icon, down.
- There are two code paths in `cmd/screentime` (`run` and `runWindow`), and a window process opens its own daemon connection. Both share `internal/shell`.
