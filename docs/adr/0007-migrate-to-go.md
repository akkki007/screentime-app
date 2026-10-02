# 7. Migrate the daemon, native host and UI shell to Go

Date: 2026-10-02

## Status

Accepted. Will supersede [ADR 2](0002-electrobun-with-a-bun-main-process.md) (Electrobun with a Bun main process) once the Wails shell reaches parity, and [ADR 6](0006-dbus-through-gdbus.md) (D-Bus through `gdbus`) once the Go daemon replaces the Bun one.

## Context

Everything we run ourselves is Bun/TypeScript: the daemon, the browser native-messaging host and the UI's main process. That has costs:

- The daemon idles at ~43 MB against a 60 MB budget, and only got there by dropping `dbus-next` for a `gdbus` subprocess, which costs up to 3 s of idle lag and a spawned process every 3 s (ADR 6).
- Every process needs a Bun runtime (~80–100 MB). Even sharing one runtime, the `.deb` is 30 MB / 86 MB installed.
- Windows and macOS focus providers, and the planned local-model planner, need native APIs and process supervision that are easier in Go.

[`docs/migration-to-go.md`](../migration-to-go.md) compares the options and gives the full plan.

## Decision

Every process we run ourselves moves to Go:

- **`screentimed`** (`cmd/screentimed`) is the daemon, and its `native-host` subcommand replaces the Bun native host. It uses `godbus/dbus` for D-Bus, `modernc.org/sqlite` (no cgo) for storage, and `jezek/xgb` for X11.
- **`screentime`** (`cmd/screentime`) is the desktop app: a Wails shell around the existing Svelte UI.

These stay as they are, because their platforms require their languages: the GNOME Shell extension (GJS), the KWin script, the browser WebExtension, and the Svelte UI itself.

These contracts stay fixed, which is what makes an incremental swap possible:
- JSON-RPC 2.0 over `$XDG_RUNTIME_DIR/screentime/daemon.sock`, with the same method names and payloads.
- The SQLite schema and migrations, tracked with `PRAGMA user_version`.
- The D-Bus interface between the GNOME extension and the daemon, apart from the revision for #3, which lands together with the Go GNOME provider.
- ADR 3, ADR 4 and ADR 5.

The migration follows the steps in `docs/migration-to-go.md`. The Bun daemon stays until the Go daemon passes golden fixtures recorded from it.

## Consequences

- Known security issues in code that Go replaces are fixed in the Go code, not patched in TypeScript. They become acceptance criteria for the matching migration step (see "Security requirements" in the migration notes). If a release is cut before the Bun daemon is removed, those fixes are backported first.
- Fixes to code that survives the migration (CI, the Svelte UI) land now. The GNOME extension fix (#3) waits for the Go provider, because it needs a persistent D-Bus connection on the daemon side.
- Contributors need Go for the core and TypeScript for the frontend and the extensions.
- macOS focus needs cgo, so macOS builds run on a macOS CI runner.
