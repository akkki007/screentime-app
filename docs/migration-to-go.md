# Migration to Go — Notes

Oct 2, 2026 · @Akshay

**Status:** accepted as [ADR 7](adr/0007-migrate-to-go.md). These notes cover what the move from Bun/TypeScript to Go involves, what it fixes, and the order to do it in so the app keeps working at every step.

## Why consider it

The architecture doc already names a native rewrite of the daemon as the fallback if Bun's footprint becomes a problem. Go is the candidate, for reasons specific to this repo:

- **D-Bus without a subprocess.** [ADR 6](adr/0006-dbus-through-gdbus.md) shells out to `gdbus` because `dbus-next` cost ~25 MB. `godbus/dbus` is pure Go and keeps one persistent connection, so the daemon can use a real `IdleMonitor` watch instead of polling `GetIdletime` every 3 s. That removes the up-to-3 s idle lag and the process spawned every poll.
- **Memory.** The daemon idles at ~43 MB against a 60 MB budget. A Go daemon doing the same work is expected to sit well below that. To be measured, not assumed.
- **Packaging and cross-OS.** One static binary per OS and architecture. Focus providers split cleanly with build tags (`focus_linux.go`, `focus_windows.go`, `focus_darwin.go`).
- **Planned AI features.** The daily planner and activity categorisation need a local model runtime that the daemon starts and stops on demand. Supervising a sidecar process is straightforward in Go.

## What stays the same

- **The IPC contract.** JSON-RPC 2.0 over `$XDG_RUNTIME_DIR/screentime/daemon.sock`, same method names and payloads. This is what makes an incremental migration possible.
- **The database.** Same `packages/db/migrations/*.sql` files, embedded with `go:embed`, still tracked with `PRAGMA user_version`. Existing users' databases open unchanged.
- **The rules model.** Everything is derived from the database and the clock on each 5 s tick ([ADR 4](adr/0004-rules-are-derived-from-the-database-on-each-tick.md)).
- **Privacy defaults** ([ADR 5](adr/0005-privacy-defaults.md)). No network calls from the daemon, titles off by default.
- **Two processes.** The daemon is always on, and the UI is launched on demand.

## What cannot be Go

Some components are written in whatever language the platform requires:

| Component | Language | Reason |
| --- | --- | --- |
| `adapters/gnome-extension` | GJS | GNOME Shell only loads JavaScript extensions |
| `adapters/kwin-script` | JS | KWin scripting API |
| `extensions/browser` | TypeScript | WebExtension APIs |
| Svelte UI (`frontend/`) | TS/Svelte | Rendered in the system webview; kept as-is (see below) |

"Fully Go" therefore means every process we run ourselves is Go: the daemon, the native messaging host, the UI's main process and the planner.

## Proposed stack

| Concern | Current | Go replacement | Notes |
| --- | --- | --- | --- |
| Runtime | Bun | Go | |
| D-Bus | `gdbus` CLI (ADR 6) | `github.com/godbus/dbus/v5` | Persistent connection, real idle watch |
| SQLite | `bun:sqlite` | `modernc.org/sqlite` | Pure Go, no cgo, keeps cross-compiling simple |
| X11 | `xprop` via `Runner` | `github.com/jezek/xgb` | Pure Go X protocol |
| Windows focus | none | `golang.org/x/sys/windows` | `GetForegroundWindow` + process name |
| macOS focus | none | cgo + NSWorkspace | The one provider that needs cgo |
| IPC schemas | Zod | JSON Schema exported from Zod, Go structs | Fixtures keep both sides honest |
| Native messaging host | Bun script | `screentimed native-host` subcommand | Same binary, no Bun at runtime |
| Desktop shell | Electrobun | Wails | Go main process + system webview; the Svelte UI moves over largely unchanged |
| Tray | Electrobun tray | Wails v3 tray (built in) | v3 is a beta; pinned at `3.0.0-beta.28`, see [ADR 9](adr/0009-wails-v3-ui-shell.md) |
| Local model runtime | none | `llama-server` (llama.cpp) as a sidecar over localhost HTTP | No cgo bindings; also serves embeddings for categorisation |

### Why Wails and not a pure-Go UI toolkit

Fyne or Gio would mean rewriting every view, chart and component. Wails uses the same WebKitGTK webview Electrobun uses on Linux, so the Svelte code carries over and only the bridge (`apps/desktop/src/shared/bridge.ts`, `mainview/lib/bridge.ts`) is rewritten against Wails bindings. [ADR 3](adr/0003-ui-reaches-the-daemon-through-its-main-process.md) still holds: the Go main process owns the socket and the webview never opens it.

### Why the daemon and UI stay separate binaries

Wails links WebKitGTK. Loading that into an always-on service would undo the memory gains, so the daemon (`screentimed`) never imports UI code.

## Proposed layout

```
cmd/screentimed/        daemon; `native-host` subcommand
cmd/screentime/         Wails desktop app
internal/store/         SQLite + embedded migrations
internal/tracker/       sessions, heartbeat, idle merge, suspend split
internal/rules/         limits, breaks, downtime, focus mode
internal/rpc/           JSON-RPC server, version handshake
internal/focus/         providers, one file per platform (build tags)
internal/notify/        org.freedesktop.Notifications
internal/planner/       AI planner (later)
frontend/               the Svelte UI, moved from apps/desktop/src/mainview
adapters/, extensions/  unchanged
packaging/              updated for Go binaries
```

## Migration plan

Each step ends with something that runs. The Bun daemon stays in the repo until the Go daemon passes step 1's fixtures.

| Step | Work | Exit criteria |
| --- | --- | --- |
| 0. Fix what survives | Fix the issues in code the migration keeps: CI hardening (#7), UI bugs (#8, #9, #10) | Issues closed |
| 1. Freeze the contract | Export Zod schemas to JSON Schema; record golden request/response fixtures from the current daemon for every RPC method and event | **Done:** [`contract/`](../contract/README.md). Fixtures checked in and replayable against any daemon via `CONTRACT_DAEMON_CMD` |
| 2. Go daemon (**done**, apart from a manual check of the GNOME extension) | Port `time.ts`, `tracker.ts` and `rules.ts` first (their unit tests also cover `event.focus`, `event.limitHit` and `event.reminder`, which the fixtures can't); then store, GNOME provider over godbus, X11, notifier, RPC server. Revise the GNOME extension's D-Bus interface together with the Go provider (#3) | All fixtures pass against `screentimed` (**done**, in CI, even with exact error messages); security requirements S1, S2, S6, S7, S8 met and tested (S1, S2, S7, S8 **done**; S6 done and tested on the Go side, the extension half awaiting a manual test in GNOME Shell) |
| 3. Swap behind the old UI (**tooling done; hardware checks open**) | Run the existing Electrobun app against the Go daemon on the same socket | UI works unchanged; 24 h soak; RSS measured and recorded |
| 4. Native host (**done**, browser check open) | Move `extensions/browser/native-host` into `screentimed native-host`; update the install script | Extension end to end with no Bun installed; S3 (client-side socket checks) met |
| 5. Remove Bun daemon (**done**) | Delete `apps/daemon` and `packages/db`; remove the GNOME extension's legacy broadcast mode; update CI, systemd unit and `.deb` | CI green with Go tests only for the daemon |
| 6. UI shell (**done**, tray and GNOME checks open) | Move the Svelte UI into a Wails app; rewrite the bridge; tray and reconnect logic in Go | Feature parity with the Electrobun app; S4 and S5 met |
| 7. New work | Planner, then Windows and macOS providers | Tracked separately |

### Porting notes

- **Migrations.** `packages/db/migrations/embed.go` embeds the same `.sql` files for Go (`go:embed` can't reach outside its package directory). They move into `internal/store` when `packages/db` is deleted in step 5.
- **Time.** Keep Unix ms UTC (`int64`) everywhere. Port `time.ts` with its tests first, because the tracker and rules depend on it.
- **Clock and runner injection.** The TypeScript code injects `now` and `Runner` for tests. Keep the same seams in Go as interfaces (`Clock`, a D-Bus connection interface) so suspend/resume and idle cases stay testable with a fake clock.
- **GVariant parsing.** `gvariant.ts` exists only because of the `gdbus` text output, so it goes away once godbus decodes messages natively.
- **Single instance.** Keep the refuse-to-start check if another daemon is already listening on the socket.
- **Socket permissions.** Still `0600` under `$XDG_RUNTIME_DIR/screentime/`, now with the checks in S2.

### Status after step 2

`cmd/screentimed` passes every contract fixture, and every Bun daemon test has a Go counterpart.

- **GNOME:** focus over godbus, subscribed to the extension (S6), and idle from Mutter watches, so there is no polling and no lag.
- **X11:** focus from `_NET_ACTIVE_WINDOW` and idle from MIT-SCREEN-SAVER, both over xgb, so `xprop` and `xprintidle` are no longer needed.
- **Notifications:** sent over godbus.
- **Tests:** the providers' integration tests run in CI against a private `dbus-daemon` and Xvfb.
- **Still open:** run the revised GNOME extension in a real Shell (checklist in the step 2c commit and `adapters/gnome-extension/README.md`), then close #3.

First memory reading (12 s idle, `none` provider, same sandbox): `screentimed` **10 MB** RSS against **63 MB** for the Bun daemon run from source. The stripped binary is 7.8 MB. The proper measurement on a real session is still step 3's.

### Status after steps 3 to 7

What is done and what only a person on a real desktop can finish. Everything below "done" was exercised in a sandbox (private session bus, Xvfb with a window manager, a fake StatusNotifier watcher), not on GNOME.

| Step | Done | Left for a real session |
| --- | --- | --- |
| 3 | `packaging/systemd/install-dev.sh` builds and installs `screentimed` as the user unit. `scripts/soak` samples RSS and checks the database for overlapping or inverted sessions. | Everything in the runbook below. |
| 4 | `screentimed native-host`, S3 (`rpc.Dial`: ownership of the socket and its directory, then `SO_PEERCRED`), installer, `.deb` manifest. Tested end to end through a pipe into a running daemon. | Firefox and Chromium with the extension loaded, on a machine without Bun. |
| 5 | Bun daemon, `packages/db` and the extension's legacy broadcast mode removed; migrations live in `internal/store/migrations`; unit, `.deb`, CI and docs updated; ADR 6 superseded. The contract fixtures pass against `screentimed` with no Bun daemon in the repo. | `dpkg -i` on a real machine. |
| 6 | Wails v3 shell ([ADR 9](adr/0009-wails-v3-ui-shell.md)): dashboard, quick panel, tray menu, S3/S4/S5, minimum window size, single instance. | Tray clicks, panel position and transparency on GNOME with the AppIndicator extension. |
| 7 | See below. | Anything needing macOS, Windows or a KDE session. |

**Measured in the sandbox** (so ceilings or floors, not session numbers): `screentimed` idles at **10 MB** RSS (budget 60 MB; was 43 MB under Bun). The app holds **75 MB** PSS with only the tray, **~500 MB** with the dashboard open under software rendering (a bare Wails window is ~290 MB; the rest is WebKitGTK), and ~150 MB after the window is closed. The `.deb` is **6.7 MB** (19 MB installed), down from 30 MB (86 MB).

**Measured on a real session** (Ubuntu 26.04, GNOME on Wayland, WebKitGTK 2.52.6, 2026-10-10):

- `screentimed` idles at **10.6 MB** RSS, flat over a 40 s sample (budget 60 MB). The 24 h reading is still to come.
- The app with the dashboard open holds about **255 MB** PSS (shell 101, WebKit web process 138, WebKit network process 16), against ~500 MB under Xvfb software rendering. This is the number to bring down before calling the app lightweight; the daemon is not the cost.
- `scripts/soak -verify-only` over the day's data: 159 sessions, 0 faults (no overlaps or inverted sessions). The one long gap (7 h 31 m) matches the machine being suspended.
- The tray item registers with the AppIndicator watcher, and the native host runs and exits cleanly with no Bun on `PATH`. Clicks, the quick panel's position and transparency, and a real browser are still to be checked by hand.

#### Runbook for the on-hardware checks (issues #24, #25, #27, and #3)

On Ubuntu with GNOME on Wayland, with `libwebkit2gtk-4.1-dev`, `libgtk-3-dev`, Go and Bun installed:

1. `packaging/systemd/install-dev.sh`, then `systemctl --user status screentime-daemon` and `journalctl --user -u screentime-daemon -f`. Install the extension (`adapters/gnome-extension/install.sh`) and log out and in.
2. `bun run dev:desktop`. Walk through Today, Trends, Apps, Limits, Wellbeing and Settings; pause and resume; focus mode; export (check `~/Downloads/*.csv` is `0600`); delete history and reset.
3. With the AppIndicator extension enabled, check the tray icon states (tracking, paused, focus, offline), the tray menu, and that a click opens the quick panel. Close the dashboard: the app should stay in the tray. Disable AppIndicator and check that closing the window then quits the UI.
4. Check the nudges: a limit (notification and overlay), a break reminder, downtime and a focus-mode nudge.
5. Run the GNOME extension checklist in `adapters/gnome-extension/README.md` (closes #3).
6. On a GNOME-on-Xorg session, check focus and idle with no `xprop` or `xprintidle` installed.
7. Soak: `go run ./scripts/soak -for 24h -every 5m`, across suspend and resume, lock and unlock, and a daemon restart (`systemctl --user restart screentime-daemon`). It reports RSS and fails on overlapping sessions; read the gaps it lists against what you did. Compare today's total with a manual count (within 1 min per hour).
8. Extension: `bun run --cwd extensions/browser build`, `install:host` for Firefox, load the extension, and check per-site time appears with no Bun on `PATH`. Repeat for Chromium with `--chromium-id`.
9. Record the RSS numbers (start and after 24 h) here and in the README, then tick the boxes on #24, #25 and #27.

## Security requirements

These come from the security review of the TypeScript code (issues #3–#7). Issues in code the migration replaces are **not** patched in TypeScript; they are fixed in the Go code, with a test for each, as part of the step named. If a release is cut before step 5, backport them to the Bun daemon first; each one is a few lines.

| ID | Requirement | Where | Step | Issue |
| --- | --- | --- | --- | --- |
| S1 | Data directory created `0700` (and `chmod`ed if it already exists); the daemon runs with `umask 077` so `screentime.db`, `-wal` and `-shm` are `0600`; systemd unit sets `UMask=0077` | `internal/store`, `packaging/systemd` | 2 | #4 |
| S2 | Before binding, `lstat` the socket directory and refuse to start unless it is a real directory owned by the current uid with no group/other bits. Create the socket under `umask 077` with no path-based `chmod`. Ignore an `XDG_RUNTIME_DIR` not owned by the user with mode `0700`, as GLib does | `internal/rpc` | 2 | #6 |
| S3 | Clients (native host, UI shell) check that the socket and its directory are owned by the current uid before connecting | `internal/rpc` client helper, used by `cmd/screentimed native-host` and `cmd/screentime` | 4, 6 | #6 |
| S4 | The UI shell attaches its daemon bridge only to the embedded frontend, never to a dev-server origin unless explicitly opted in (e.g. `SCREENTIME_DEV_SERVER=1`). Probing a port is not enough to trust it | `cmd/screentime` | 6 | #5 |
| S5 | Exports are written `0600` with exclusive create (`O_EXCL`), and the filename is reduced to its base name | `cmd/screentime` | 6 | #4 |
| S6 | Revise the extension's D-Bus interface: a `Subscribe()` method records the daemon's unique name, and `FocusChanged` is sent only to it, never broadcast. Titles are `''` until the daemon calls `SetCaptureTitles(true)`. `GetFocus` returns a title only to the subscriber. On the Go provider side: subscribe over a persistent godbus connection, send `SetCaptureTitles` when the setting changes, and check that each `FocusChanged` comes from the extension's unique name. Needs a persistent connection, which the `gdbus`-based Bun daemon doesn't have, so it isn't fixed before this step | `adapters/gnome-extension`, `internal/focus` | 2 | #3 |
| S7 | Every RPC request is size-capped (as the TS server's 1 MB line cap is) and validated; numeric params have upper bounds (e.g. `tracker.pause.minutes`) | `internal/rpc` | 2 | — |
| S8 | Go CI follows #7: `permissions: contents: read`, actions pinned to SHAs, Go version taken from `go.mod`; add `govulncheck` | `.github/workflows` | 2 | #7 |

## Risks

| Risk | Impact | Mitigation |
| --- | --- | --- |
| Behaviour drift during the rewrite | Wrong totals or limits firing differently | Golden fixtures from the Bun daemon; port the existing tests before porting the code |
| Wails v3 maturity | Tray or Linux quirks | Pinned version; the UI is a thin client and the shell a thin layer over `internal/shell`, so it stays swappable |
| cgo on macOS | Cross-compiling from Linux gets harder | Build macOS in CI on a macOS runner |
| Two stacks during transition | More for contributors to set up | Keep the transition short; steps 2–5 are the critical path |
| One-language promise changes | Contributors now need Go for the core and TS for the UI | Go is the core; TS is limited to the frontend and platform-mandated extensions |

## Open questions

- [x] Wails v2 or v3: **v3** (beta.28, `-tags gtk3`, pinned); see [ADR 9](adr/0009-wails-v3-ui-shell.md)
- [x] ~~Generate Go types from `contract/schema/rpc.schema.json`, or write them by hand?~~ By hand: the contract fixtures check every field, and generated code from Zod's JSON Schema is awkward Go.
- [ ] Should `architecture.md` and ADR 2 be updated in the same PR as the decision, or once the Go daemon reaches parity?
- [x] ~~Minimum Go version~~ `go 1.26.0` (the oldest supported release when step 2 started), with `toolchain go1.27.1` pinned in `go.mod`. CI reads both from there.

## Next step

Run the runbook above on a real session, record the numbers, and close #3, #24, #25 and #27. Then step 7: the planner needs its ADR accepted first ([ADR 8](adr/0008-local-planner-sidecar.md)).
