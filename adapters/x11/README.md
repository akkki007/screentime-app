# X11 focus adapter

**Status: v1.x, implemented in the daemon** (`internal/focus/x11.go`). Nothing to install: there is no script or extension for X11.

- **Focus source:** `_NET_ACTIVE_WINDOW` on the root window, over the X protocol (`github.com/jezek/xgb`). `WM_CLASS` is resolved to the desktop-entry ID, so an app has the same ID as under Wayland.
- **Idle source:** the MIT-SCREEN-SAVER extension's idle time.
- Used for X11 sessions, including GNOME on Xorg. It is integration-tested in CI against a private Xvfb server; it still needs a check on a real X11 session (see `docs/migration-to-go.md`).

See `docs/adapters.md`.
