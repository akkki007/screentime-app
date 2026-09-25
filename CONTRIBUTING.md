# Contributing

Thanks for considering a contribution to Screentime! This project is a Bun workspaces monorepo, entirely TypeScript/JavaScript (plus GJS for the GNOME adapter), so you only need one toolchain to work across the whole stack.

## Setup

```bash
bun install
```

Each package under `apps/`, `adapters/`, `extensions/`, and `packages/` can be built and tested independently.

## Workflow

1. Fork the repo and create a branch off `main`.
2. Make your change. Keep pull requests focused — one concern per PR.
3. Run the checks locally before opening a PR:
   ```bash
   bun run lint
   bun run typecheck
   bun test
   ```
4. Use [Conventional Commits](https://www.conventionalcommits.org/) for commit messages (`feat:`, `fix:`, `docs:`, `refactor:`, `chore:`, ...).
5. Open a PR against `main`. CI runs lint, typecheck, tests, and a build of every package.

## Where to start

Look for issues labeled [`good first issue`](../../labels/good%20first%20issue). High-value, well-scoped areas for new contributors:

- **New desktop adapters** — implement the `FocusProvider` interface (see `packages/shared/src/focus.ts`) for a new compositor or window manager. See [`docs/adapters.md`](docs/adapters.md).
- **App categories** — help classify common Linux apps as productive/unproductive.
- **UI translations** — the Svelte dashboard in `apps/desktop`.

## Design principles to respect

- **Local-only.** Never add a network call to the daemon.
- **The daemon owns data; the UI is a thin client.** Business logic (session merging, limits, rules) lives in `apps/daemon`, not in the UI.
- **Adapters implement one shared interface.** Don't special-case a desktop environment outside of its adapter.
- **Privacy by default.** Anything that could capture sensitive data (e.g. window titles) must be opt-in.

## Reporting bugs / requesting features

Use the issue templates. For security issues, see [SECURITY.md](SECURITY.md) instead of opening a public issue.

## Code of Conduct

This project follows the [Code of Conduct](CODE_OF_CONDUCT.md). Please read it before participating.
