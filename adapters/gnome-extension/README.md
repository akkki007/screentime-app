# GNOME Shell focus adapter

A small GJS (ESM, GNOME 45+) Shell extension that reports the focused window over D-Bus. This is the v1 adapter — the only reliable focus source on GNOME Wayland — consumed by `apps/daemon/src/focus-providers/gnome-wayland.ts`.

It intentionally does nothing else: no tracking, no storage, no network access. Keeping it tiny is a deliberate mitigation against GNOME Shell API changes breaking it on upgrade (see `docs/architecture.md#risks-and-open-questions`).

`metadata.json` declares GNOME 45–50. It has only been run on GNOME 50.1; please test the others before relying on that range.

## Install (development)

```bash
adapters/gnome-extension/install.sh              # symlink into ~/.local/share/gnome-shell/extensions and try to enable
adapters/gnome-extension/install.sh --uninstall
```

On Wayland, **log out and back in** once so GNOME Shell discovers a newly installed extension (and again after editing `extension.js`, which Shell caches), then `gnome-extensions enable screentime-focus@akkki007.github.io`. On X11, `Alt+F2`, `r`, Enter.

Installed from the `.deb`, it lives in `/usr/share/gnome-shell/extensions/` and only needs enabling.

GNOME marks the extension *inactive* while the screen is locked. That is expected; the daemon reconnects when it comes back.

## D-Bus contract

Owns the name `io.github.akkki007.screentime` and exports `/io/github/akkki007/screentime/Focus` with interface `io.github.akkki007.screentime.Focus`:

- signal `FocusChanged(s appId, s title, u pid)` whenever the focused window changes;
- method `GetFocus() -> (s appId, s title, u pid)` returning the current window (empty `appId` if none), so a daemon that starts later can catch up.

`appId` is the `.desktop` file ID, falling back to `WM_CLASS` for windows with no desktop entry.

Window titles are emitted here, but the daemon discards them unless the user has opted in — treat `title` as always-present-but-usually-discarded, not privacy-safe by itself.
