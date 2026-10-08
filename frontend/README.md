# Desktop app

The dashboard and tray: an [Electrobun](https://blackboard.sh/electrobun) app (WebKitGTK) with a **Bun main process** and a **Svelte 5 + Tailwind 4** UI. It is a thin client of the daemon; it never touches SQLite. See [ADR 2](../../docs/adr/0002-electrobun-with-a-bun-main-process.md) and [ADR 3](../../docs/adr/0003-ui-reaches-the-daemon-through-its-main-process.md).

```
src/bun/        main process: window, tray, DaemonConnection (auto-reconnect), bridge handlers
src/shared/     the webview <-> main-process RPC schema
src/mainview/   the Svelte UI (views/, components/, lib/)
```

## Develop

The daemon must be running for real data (`bun run dev:daemon` from the repo root).

```bash
bun run dev      # build the UI, then launch the app (rebuilds on change)
bun run hmr      # Vite dev server on :5173 for hot reload; launch the app in a second terminal
bun run test     # unit tests
bun run typecheck
```

The first run downloads the Hutch/Electrobun toolchain into `~/.hutch`.

### Preview without Electrobun or a daemon

With the Vite server running (`bun run hmr`) open <http://localhost:5173> in any browser. Outside Electrobun the UI uses an in-memory mock daemon with seeded fixture data, so every screen can be developed and screenshotted. Query parameters help:

| Parameter | Effect |
| --- | --- |
| `?view=week` | start on `today`, `week`, `apps`, `limits`, `wellbeing` or `settings` |
| `?theme=dark` | force a theme (otherwise follows the system) |
| `?onboarding=1` | show the first-run flow |
| `?disconnected=1` | show the daemon-offline state |
| `?limitHit=1` / `?toast=1` | trigger the limit overlay / a reminder toast |

The mock is `src/mainview/lib/mock.ts`. Keep it in step with `packages/shared/src/ipc.ts`.

To screenshot a view headlessly (this is how `docs/media` was made; it needs Firefox and captures only the page):

```bash
bun run screenshot -- out.png "http://localhost:5173/?view=limits&theme=dark" 1280x700 1800
bun run screenshot -- out.png "http://localhost:5173/?view=limits" 1280x700 1500 "text=New limit"   # click first
```

## Build

```bash
bun run build    # production build into build/ and artifacts/
```

`packaging/deb/build.sh` turns that into a `.deb`.

## Notes

- Closing the window leaves the app in the tray (`exitOnLastWindowClosed: false`). GNOME needs the AppIndicator extension to show a tray icon.
- Hutch's script runner cannot see workspace-hoisted binaries, so build steps live in `package.json`, not `hutch.config.ts`.
- `.hutch/`, `build/`, `dist/` and `artifacts/` are generated and gitignored.
