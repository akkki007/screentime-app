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
| `gnome-wayland` | Own GNOME Shell extension, emits `FocusChanged` over D-Bus | `org.gnome.Mutter.IdleMonitor` | v1, tested on GNOME 50.1 |
| `x11` | `_NET_ACTIVE_WINDOW` via `xprop -spy` | `xprintidle` (XScreenSaver) | v1.x, unit-tested only (no X11 session to run it on) |
| `kde-wayland` | KWin script over D-Bus | `org.freedesktop.ScreenSaver` | v2 |
| `hyprland` / `sway` | Compositor IPC socket events | `ext-idle-notify-v1` | v2 |

## Notes on the existing adapters

**`gnome-wayland`.** The extension owns the bus name `io.github.akkki007.screentime` and exposes both the `FocusChanged` signal and a `GetFocus()` method, so a daemon that starts after the focused window was last changed still learns it. GNOME deactivates extensions on the lock screen and loads them in parallel with login, so the daemon never depends on the extension being present at start: `gdbus monitor` reports the name appearing and disappearing, and each time it appears the daemon calls `GetFocus` to catch up. Idle is read by polling Mutter's `GetIdletime` every 3 s (an idle *watch* would die with each one-shot `gdbus` connection). Windows without a `.desktop` file get a synthetic `window:<n>` ID from GNOME; the extension falls back to `WM_CLASS` and the daemon folds any that slip through into `unknown`.

**`x11`.** Uses `xprop` and `xprintidle` rather than an X client library, keeping the daemon pure TypeScript. `WM_CLASS` is resolved to the desktop-entry ID through `StartupWMClass`, so an app has the same ID as under Wayland. Without `xprintidle` there is no idle detection and the daemon warns at startup. This is the adapter used for GNOME on Xorg too.

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

Automated tests should mock the transport and assert on the `FocusProvider` events emitted, rather than requiring a real session. `apps/daemon/src/x11.test.ts` is the model: the provider takes a `Runner` for external commands, so tests script the tool output (`xprop` lines, `xprintidle` values) and check ordering, unsubscribe and idle transitions. Keep parsers in pure modules (`x11-parse.ts`) so they are testable on their own.
