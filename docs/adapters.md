# Adding a desktop adapter

The tracker daemon is desktop-agnostic. All desktop-specific focus/idle detection lives behind a single interface, `FocusProvider`, defined in `packages/shared/src/focus.ts`:

```ts
interface FocusProvider {
  id: string;                       // "gnome-wayland"
  isAvailable(): Promise<boolean>;
  onFocusChange(cb: (w: FocusedWindow) => void): Unsubscribe;
  onIdleChange(cb: (idle: boolean) => void, thresholdMs: number): Unsubscribe;
}
type FocusedWindow = { appId: string; title?: string; pid?: number; ts: number };
```

The daemon picks an adapter at startup based on `$XDG_SESSION_TYPE` and `$XDG_CURRENT_DESKTOP`, in priority order (see `apps/daemon/src/focus-providers`).

## Existing adapters

| Adapter | Focus source | Idle source | Status |
| --- | --- | --- | --- |
| `gnome-wayland` | Own GNOME Shell extension, emits `FocusChanged` over D-Bus | `org.gnome.Mutter.IdleMonitor` | v1 |
| `x11` | `_NET_ACTIVE_WINDOW` via xcb / `bun:ffi` | XScreenSaver extension | v1.x |
| `kde-wayland` | KWin script over D-Bus | `org.freedesktop.ScreenSaver` | v2 |
| `hyprland` / `sway` | Compositor IPC socket events | `ext-idle-notify-v1` | v2 |

## Adding a new one

1. Create a new directory under `adapters/` for anything that must run outside the daemon process (a Shell extension, a compositor script, etc.), or a new file under `apps/daemon/src/focus-providers/` if the adapter can talk to the OS directly from the daemon (e.g. via `bun:ffi` or a D-Bus interface that already exists, like X11's).
2. Implement `FocusProvider`. `isAvailable()` should check session type/desktop and any required binaries/extensions, returning `false` rather than throwing when the adapter doesn't apply.
3. Register the adapter in the daemon's provider selection logic, in priority order for its session type.
4. Add app-identity mapping: `appId` should resolve to the `.desktop` file ID (e.g. `org.mozilla.firefox`), falling back to `WM_CLASS` when no `.desktop` file matches.
5. Respect the window-title opt-in — only populate `title` when the user has explicitly enabled it in settings.
6. Add a short section to this doc's adapter table above.

## Testing an adapter

Since adapters depend on a live desktop session, they're best tested manually:

```bash
bun run --cwd apps/daemon dev
# focus different windows / go idle, and watch the daemon log FocusChanged / idle events
```

Automated tests should mock the D-Bus/IPC transport and assert on the `FocusProvider` events emitted, rather than requiring a real session.
