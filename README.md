# Screentime

An open-source, local-first screentime and digital wellbeing app for Linux: track app usage, see insights, get nudged to take breaks, and enforce limits — all without your data ever leaving your machine.

> **Status:** early scaffolding (Phase 0 — spike). See the [roadmap](docs/architecture.md#roadmap) for what's next.

<!--
TODO: add a GIF of the dashboard here once Phase 2 ships.
![Screentime dashboard demo](docs/media/demo.gif)
-->

## Why

Most screentime tools are either mobile-only, cloud-connected, or don't work properly under Wayland. This project tracks your usage entirely on-device, with no telemetry and no account, and is built to work well specifically on GNOME/Wayland first — the hardest and most common case for modern Linux desktops.

## Principles

- **Local-only data.** Everything lives in `~/.local/share/screentime/`. Nothing is uploaded anywhere.
- **No telemetry.** The daemon never makes a network call.
- **Low idle footprint.** The tracker is a lightweight background service, not an Electron app running 24/7.
- **Pluggable per-desktop adapters.** GNOME Wayland first, then KDE Wayland, wlroots compositors, and X11.

**Non-goals for v1:** cloud sync, mobile apps, multi-user parental controls.

## Architecture

A background tracker daemon owns all data; the desktop UI is only a client and tracking keeps running while the UI is closed. See [`docs/architecture.md`](docs/architecture.md) for the full design, data model, and IPC contracts.

```
GNOME Shell extension ─┐
Mutter IdleMonitor ─────┼─ D-Bus ──> Tracker daemon (systemd --user) ──> SQLite
Browser extension ──────┘                    │
                                       Unix socket JSON-RPC
                                              │
                                        Electrobun UI
```

## Repo structure

```
apps/desktop/        Electrobun app (Svelte UI + tray)
apps/daemon/          tracker daemon, rules engine, RPC server
adapters/             per-desktop focus/idle adapters (GNOME, X11, KWin)
extensions/browser/    WebExtension + native messaging host
packages/shared/       Zod schemas, RPC types, constants
packages/db/           SQLite migrations and queries
packaging/             systemd unit, .desktop file, deb/AppImage scripts
docs/                  architecture, ADRs, adapter guide
```

## Getting started

Requires [Bun](https://bun.sh) >= 1.1 on Linux.

```bash
bun install
bun run dev:daemon   # start the tracker daemon
bun run dev:desktop  # start the dashboard UI
```

## Contributing

Contributions are very welcome — see [CONTRIBUTING.md](CONTRIBUTING.md). Good first contributions include new desktop adapters, app categories, and UI translations; look for issues labeled `good first issue`.

## Privacy promise

- No network calls from the daemon, ever.
- All data stays in `~/.local/share/screentime/`.
- Window titles are off by default (opt-in only).
- One-click data wipe from the dashboard settings.

## License

[GPL-3.0-or-later](LICENSE).
