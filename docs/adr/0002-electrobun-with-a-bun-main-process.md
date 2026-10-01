# 2. Electrobun with a Bun main process, built through Hutch

Date: 2026-10-01

## Status

Accepted

## Context

`docs/architecture.md` chose Electrobun (Bun + WebKitGTK) for the dashboard, with Tauri as the fallback. The Phase 0 spike found that Electrobun 2.x differs from what the doc assumed:

- Projects are built and run by **Hutch**, a separate toolchain that the `electrobun` npm package bootstraps into `~/.hutch`.
- The main process now **defaults to Cottontail**, not Bun. `build.mainProcess: "bun"` is still supported.
- Hutch's script runner only sees project-local `node_modules/.bin`, but this repo is a Bun workspace where dependencies such as `vite` are hoisted, so scripts in `hutch.config.ts` can't call them.
- The production launcher is a *self-installer*: on first run it unpacks itself under `~/.local/share` and creates desktop shortcuts.

## Decision

- Use `mainProcess: "bun"`. The main process needs a Unix socket to the daemon and shares types with it; `Bun.connect` is what the rest of the repo already uses, and it keeps the stack to one runtime.
- Set `packageManager: "bun"` in `hutch.config.ts` so `workspace:*` dependencies resolve, and keep build steps in `package.json` scripts (`bun run build:view && electrobun prepare && electrobun dev`), invoked through the `electrobun` npm package, which delegates to Hutch and bootstraps it on first use, so `hutch` need not be on `PATH` rather than Hutch scripts.
- Set `runtime.exitOnLastWindowClosed: false`: closing the window leaves the UI in the tray, as the daemon already tracks independently.
- For the `.deb`, unpack Electrobun's archive ourselves instead of running its self-installing launcher, so a system package never writes into a user's home or desktop.

## Consequences

- Contributors need Hutch for the desktop app (`bunx electrobun` bootstraps it). The rest of the repo needs only Bun.
- The `.hutch/` devkit is generated and gitignored; it must be excluded from linting (`biome.json`) and `hutch electrobun prepare` runs before type checking.
- Electrobun is still young. The UI is a thin client of the daemon, so switching shells (Tauri) remains cheap if this becomes a problem.
