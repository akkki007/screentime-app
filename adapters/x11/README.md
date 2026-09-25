# X11 focus adapter

**Status: v1.x — not started.** See `docs/architecture.md#roadmap` (Phase 4) and `docs/adapters.md`.

Planned implementation:

- **Focus source:** `_NET_ACTIVE_WINDOW` on the root window, via `xcb` or `bun:ffi` against `libX11`/`libxcb`.
- **Idle source:** the XScreenSaver (`XSS`) extension's idle time query.
- Implements the shared `FocusProvider` interface from `packages/shared/src/focus.ts`, registered in `apps/daemon/src/focus-providers`.
