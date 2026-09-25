# GNOME Shell focus adapter

A small GJS (ESM, GNOME 45+) Shell extension that reports the focused window over D-Bus. This is the v1 adapter — the only reliable focus source on GNOME Wayland — consumed by `apps/daemon/src/focus-providers/gnome-wayland.ts`.

It intentionally does nothing else: no tracking, no storage, no network access. Keeping it tiny is a deliberate mitigation against GNOME Shell API changes breaking it on upgrade (see `docs/architecture.md#risks-and-open-questions`).

## Install (development)

```bash
ln -s "$(pwd)" ~/.local/share/gnome-shell/extensions/screentime-focus@akkki007.github.io
# Wayland: log out/in to reload the shell. X11: Alt+F2, r, Enter.
gnome-extensions enable screentime-focus@akkki007.github.io
```

## D-Bus contract

Emits `io.github.akkki007.screentime.Focus.FocusChanged(s appId, s title, u pid)` on the session bus at `/io/github/akkki007/screentime/Focus` whenever the focused window changes.

Window titles are emitted here, but the daemon only surfaces them to the UI when the user has opted in — treat `title` as always-present-but-often-discarded, not privacy-safe by itself.
