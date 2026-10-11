# 3. The UI reaches the daemon through its main process

Date: 2026-10-01

## Status

Accepted. Still holds with a Go main process ([ADR 9](0009-wails-v3-ui-shell.md)); the details below describe the Electrobun implementation, which `internal/shell` and `cmd/screentime` replaced. The allow-list is now an explicit list in `internal/shell/bridge.go`, and exports follow S5 (`0600`, exclusive create, base name only).

## Context

The dashboard has two halves: a Bun main process and a Svelte webview in WebKitGTK. A webview cannot open Unix sockets. We also want to develop and screenshot the UI without a daemon, and to avoid giving web content a broad channel to the machine.

## Decision

- The webview talks to the main process over one narrow Electrobun RPC (`src/shared/bridge.ts`): a single `daemon({method, params})` request, a `saveExport` request, and two push messages (`daemonEvent`, `connection`).
- The main process owns the socket (`DaemonConnection`, which reconnects automatically) and **forwards only method names present in the shared `RpcMethods` table**. Daemon notifications are relayed to the webview.
- Request and response types in the webview come from the same Zod schemas the daemon validates with (`z.input` / `z.output` of `RpcMethods`), so the two cannot drift silently.
- When the page runs outside Electrobun (plain browser), the bridge falls back to an in-memory mock daemon with seeded fixture data. It is loaded by dynamic import and never ships in the app path.

## Consequences

- The UI can be developed with `bun run --cwd apps/desktop hmr` and any browser, and screenshots are reproducible.
- The mock must be kept in step with the RPC contract by hand; it is only for development, and the real bridge is exercised by running the app.
- Exports are written by the main process to `~/Downloads` (never overwriting, and ignoring any path component in the filename the daemon returns).
