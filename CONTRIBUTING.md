# Contributing

Thanks for considering a contribution to Screentime! The tracker daemon is Go; the dashboard and browser extension are TypeScript (a Bun workspaces monorepo), plus GJS for the GNOME adapter. You only need the toolchain for the part you touch.

## Setup

```bash
bun install
go run ./cmd/screentimed   # tracker daemon (needs the GNOME extension on GNOME/Wayland)
bun run dev:desktop  # builds the Wails app (needs libwebkit2gtk-4.1-dev and libgtk-3-dev) and opens it
```

The dashboard can also be developed with no daemon and no desktop shell, in a plain browser: see [`frontend/README.md`](frontend/README.md#preview-in-a-browser-without-the-app-or-a-daemon).

The Go packages under `internal/` and the TypeScript packages under `frontend/`, `extensions/` and `packages/` can be built and tested independently. `cmd/screentime` needs `-tags gtk3` (`go build -tags gtk3 ./cmd/screentime`), which `bun run build:app` does for you.

## Workflow

1. Fork the repo and create a branch off `main`.
2. Make your change. Keep pull requests focused — one concern per PR.
3. Run the checks locally before opening a PR:
   ```bash
   gofmt -l . && go vet ./... && go test ./...
   bun run lint
   bun run typecheck
   bun run test
   bun run test:contract   # if you touched the daemon's RPC behaviour
   ```
4. Use [Conventional Commits](https://www.conventionalcommits.org/) for commit messages (`feat:`, `fix:`, `docs:`, `refactor:`, `chore:`, ...).
5. Open a PR against `main`. CI runs lint, typecheck, tests, and a build of every package.

## Where to start

Look for issues labeled [`good first issue`](../../labels/good%20first%20issue). High-value, well-scoped areas for new contributors:

- **New desktop adapters** — implement the `focus.Provider` interface (see `internal/focus/focus.go`) for a new compositor or window manager. See [`docs/adapters.md`](docs/adapters.md).
- **App categories** — help classify common Linux apps as productive/unproductive.
- **UI translations** — the Svelte dashboard in `frontend/`.
- **Verification we could not do** — the browser extension in a real browser, the X11 adapter on an X11 session, the GNOME extension on GNOME 45–49, a real suspend/resume, and a 24 h accuracy run. See the status table in `docs/architecture.md`.

## Design principles to respect

- **Local-only.** Never add a network call to the daemon.
- **The daemon owns data; the UI is a thin client.** Business logic (session merging, limits, rules) lives in the daemon (`internal/`), not in the UI.
- **Adapters implement one shared interface.** Don't special-case a desktop environment outside of its adapter.
- **Privacy by default.** Anything that could capture sensitive data (e.g. window titles) must be opt-in.

## Reporting bugs / requesting features

Use the issue templates. For security issues, see [SECURITY.md](SECURITY.md) instead of opening a public issue.

## Code of Conduct

This project follows the [Code of Conduct](CODE_OF_CONDUCT.md). Please read it before participating.
