# Migration to Go — Notes

Oct 2, 2026 · @Akshay

**Status:** proposal. Nothing here is decided until it is written up as an ADR. These notes collect what a move from Bun/TypeScript to Go would involve, what it would fix, and the order to do it in so the app keeps working at every step.

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
| Svelte UI (`apps/desktop/src/mainview`) | TS/Svelte | Rendered in the system webview; kept as-is (see below) |

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
| Tray | Electrobun tray | Wails v3 tray, or a systray library on Wails v2 | Check Wails v3 release status before choosing |
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
frontend/               current Svelte UI, moved from apps/desktop/src/mainview
adapters/, extensions/  unchanged
packaging/              updated for Go binaries
```

## Migration plan

Each step ends with something that runs. The Bun daemon stays in the repo until the Go daemon passes step 1's fixtures.

| Step | Work | Exit criteria |
| --- | --- | --- |
| 1. Freeze the contract | Export Zod schemas to JSON Schema; record golden request/response fixtures from the current daemon for every RPC method and event | Fixtures checked in and replayable |
| 2. Go daemon | Port `time.ts`, `tracker.ts` and `rules.ts` first (pure logic, tests port as table-driven tests); then store, GNOME provider over godbus, X11, notifier, RPC server | All fixtures pass against `screentimed` |
| 3. Swap behind the old UI | Run the existing Electrobun app against the Go daemon on the same socket | UI works unchanged; 24 h soak; RSS measured and recorded |
| 4. Native host | Move `extensions/browser/native-host` into `screentimed native-host`; update the install script | Extension end to end with no Bun installed |
| 5. Remove Bun daemon | Delete `apps/daemon` and `packages/db`; update CI, systemd unit and `.deb` | CI green with Go tests only for the daemon |
| 6. UI shell | Move the Svelte UI into a Wails app; rewrite the bridge; tray and reconnect logic in Go | Feature parity with the Electrobun app |
| 7. New work | Planner, then Windows and macOS providers | Tracked separately |

### Porting notes

- **Time.** Keep Unix ms UTC (`int64`) everywhere. Port `time.ts` with its tests first, because the tracker and rules depend on it.
- **Clock and runner injection.** The TypeScript code injects `now` and `Runner` for tests. Keep the same seams in Go as interfaces (`Clock`, a D-Bus connection interface) so suspend/resume and idle cases stay testable with a fake clock.
- **GVariant parsing.** `gvariant.ts` exists only because of the `gdbus` text output, so it goes away once godbus decodes messages natively.
- **Single instance.** Keep the refuse-to-start check if another daemon is already listening on the socket.
- **Socket permissions.** Still `0600` under `$XDG_RUNTIME_DIR/screentime/`.

## Risks

| Risk | Impact | Mitigation |
| --- | --- | --- |
| Behaviour drift during the rewrite | Wrong totals or limits firing differently | Golden fixtures from the Bun daemon; port the existing tests before porting the code |
| Wails v3 maturity | Tray or Linux quirks | Check the release status first; the UI is a thin client, so the shell stays swappable |
| cgo on macOS | Cross-compiling from Linux gets harder | Build macOS in CI on a macOS runner |
| Two stacks during transition | More for contributors to set up | Keep the transition short; steps 2–5 are the critical path |
| One-language promise changes | Contributors now need Go for the core and TS for the UI | Go is the core; TS is limited to the frontend and platform-mandated extensions |

## Open questions

- [ ] Wails v2 or v3, depending on v3's status when step 6 starts
- [ ] Generate Go types from JSON Schema, or write them by hand and rely on fixtures?
- [ ] Should `architecture.md` and ADR 2 be updated in the same PR as the decision, or once the Go daemon reaches parity?
- [ ] Minimum Go version (matters for `go:embed`, generics and `slog`)

## Next step

If the direction is accepted, record it as **ADR 7: Migrate the daemon to Go** and start step 1.
