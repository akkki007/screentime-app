# 14. A notch at the top of the screen, drawn natively on each desktop

Date: 2026-10-31

## Status

Accepted.

## Context

Screentime talks to you in three places today: desktop notifications, the tray menu and the quick panel. Notifications pile up in a tray of their own and the panel has to be opened. We want one calm place, always in the same spot, that shows the state of your day and speaks up at the moment it matters: a black pill docked to the top centre of the screen (a "notch"), that expands when clicked.

It is visible all day, so its cost is paid all day. A webview notch would hold WebKitGTK's ~250 MB for as long as you are logged in ([ADR 10](0010-windows-run-as-their-own-processes.md) measured an open window), which ends the "lightweight" claim. On GNOME Wayland an ordinary window also cannot be placed at the top centre or kept above others; only the Shell itself can draw there.

## Decision

**The notch is native on every desktop, and every notch is an ordinary client of the daemon's RPC socket.** It needs nothing the daemon does not already offer: `event.status`, `event.focus`, `event.limitHit` and `event.reminder` for what happens, `usage.summary`, `apps.list` and `limits.list` for the numbers, and `tracker.pause`, `tracker.resume`, `focus.start` and `focus.stop` for its buttons. Like the UI shell it checks that the socket and its directory belong to the user before connecting (S3).

| Desktop | Drawn by | Placement |
| --- | --- | --- |
| GNOME (Wayland and Xorg) | the Screentime Shell extension, in the Shell's own process (St widgets) | hangs from the bottom of the top bar, centred |
| KDE Plasma, Sway, Hyprland and other wlroots desktops | a GTK window in the tray process, as a `wlr-layer-shell` surface (`libgtk-layer-shell`, loaded if installed) | anchored to the top edge |
| Other X11 desktops | the same GTK window as a dock-type, keep-above window | top centre of the primary monitor |
| Windows | `screentime-notch.exe`, a Win32 layered, topmost, tool window (no cgo) | top centre of the primary monitor's work area |
| macOS | `screentime-notch`, a borderless `NSPanel` (cgo) | top centre, under the menu bar or around the camera notch |

On Linux the GTK notch lives in the tray process, which already has GTK loaded, so it adds a few MB rather than a process. Where the extension draws the notch (GNOME), the tray process does not.

The GTK notch as built (Linux, tray process): the window is created only while `notchEnabled` is on and destroyed when it is turned off, so the setting also gives the memory back. Measured on Ubuntu 26.04 against the tray without it: about +0.3 MB PSS on X11 and +0.9 MB under `wlr-layer-shell`. The layer surface is made once at the size of the largest view (the panel with four apps) and never resized: sway drops the keyboard from an on-demand surface whose size changes, which lost Escape the moment the panel opened. It is drawn inside that surface and input is limited to what is drawn; a dock window is resized and moved instead. While the panel is open an X11 dock window holds a pointer and keyboard grab, because the window manager never focuses a dock and a click elsewhere would not otherwise close the panel. Without a compositor the window is cut to the notch's shape rather than made transparent.

### What it shows

One shared model, `internal/notch` (Go) and `adapters/gnome-extension/notch.js` (JavaScript, for the Shell), turns those events into what the pill shows. Both are tested against the same fixtures (`internal/notch/testdata/fixtures.json`), so the notch says the same thing on every desktop.

The pill shows the highest-priority view that applies:

| Priority | View | Icon | Text | Detail | How long |
| --- | --- | --- | --- | --- | --- |
| 1 | A limit was reached | limit | `Daily limit reached` | `<app or category> · <limit>` | 6 s |
| 2 | A reminder | break / moon / bolt | `Time for a break` / `Downtime` / `Stay focused` | the daemon's message, at most 60 characters | 6 s |
| 3 | The first activity of the day | wave | `Good morning, Alex!` (morning before 12:00, afternoon before 18:00, then evening; no name if the system has none) | | 5 s |
| 4 | Not connected to the daemon | offline | `Screentime is not running` | | while it lasts |
| 5 | Focus mode is on | timer | the time left, `24:13` (`1:05:00` from an hour) | `Focus` | until it ends |
| 6 | Tracking is paused | pause | `Paused` | `until 14:05`, or nothing | while it lasts |
| 7 | Otherwise | clock | today's total, `3h 17m` | | always |

A newer event replaces a shown one of the same or lower priority. Durations use the dashboard's short form (`<1m`, `45m`, `3h`, `3h 17m`).

Clicking the pill opens a compact panel below it: today's total, the four most-used apps with their share, and three actions: *Pause 15 min* (or *Resume*), *Focus 25 min* (or *Stop focus*) and *Open Screentime*, which launches the dashboard window. Escape or a click elsewhere closes it.

### How it looks

Black (`#0c0a09`), white text, the bottom corners rounded and concave shoulders where it meets the top edge, so it reads as part of the screen rather than a floating window. The collapsed pill is about 34 px high; temporary views grow it sideways with a short ease, which is turned off when the desktop asks for reduced motion. The app's orange is used only for the focus progress and the status dot.

### Privacy and weight

- The notch shows app names and totals, never window titles, whatever the title setting says.
- It holds one socket connection and redraws only when an event arrives or, for focus mode, once a second while focus is on. With nothing happening it does no work.
- The greeting uses the account's real name as the system reports it; nothing is stored.
- A `notchEnabled` setting (default on) turns it off everywhere; the extension and the tray process both follow it.

## Consequences

- Five small implementations instead of one, held together by one model and one set of fixtures. The JavaScript model is a port of the Go one; a fixture failing in one and passing in the other is a bug.
- Nothing is resident that was not already: the Shell, the tray process, or one small native process on Windows and macOS.
- GNOME users get the notch with the extension they already install. Other Linux desktops get it from the tray process; layer-shell placement needs `libgtk-layer-shell0`, and without it the notch falls back to an ordinary keep-above window.
- Windows and macOS gain their first piece of UI before the full app reaches them. They are built and unit-tested in CI, but neither has been run on a real machine yet.
