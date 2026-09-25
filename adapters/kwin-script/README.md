# KWin focus adapter (KDE Wayland)

**Status: v2 — not started.** See `docs/architecture.md#roadmap` (Phase 6) and `docs/adapters.md`.

Planned implementation:

- **Focus source:** a KWin script that calls the daemon over D-Bus on window activation.
- **Idle source:** `org.freedesktop.ScreenSaver`.
- Implements the shared `FocusProvider` interface from `packages/shared/src/focus.ts`, registered in `apps/daemon/src/focus-providers`.
