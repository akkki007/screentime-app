# 12. wlroots adapter: focus from the foreign-toplevel protocol, idle from ext-idle-notify-v1

Date: 2026-10-28

## Status

Accepted. Not yet run on a real compositor; see "Left to check" below. Part of step 7 of the Go migration (#28), after the KDE adapter ([ADR 11](0011-kde-wayland-adapter.md)).

## Context

sway, Hyprland, river, labwc, Wayfire and the other wlroots-based compositors have no shell extension or script host to run code for us, and no D-Bus API for the focused window. The roadmap planned a separate adapter per compositor over its IPC socket (sway's i3 IPC, Hyprland's `.socket2.sock`).

They all implement `zwlr_foreign_toplevel_management_unstable_v1`, which exists so that taskbars can list and activate windows. A plain Wayland client may bind it, with no privileges.

## Decision

One `wlroots` provider (`internal/focus/wlroots.go`) replaces the per-compositor adapters.

**Focus.** The provider binds `zwlr_foreign_toplevel_manager_v1` (version 1) on its own connection. The compositor then sends a `toplevel` event with a new `zwlr_foreign_toplevel_handle_v1` for every existing window, and for each new one. A handle sends `title`, `app_id` and `state` (an array of `u32`; `activated` is 2), and then `done`, which is when those changes take effect. `closed` ends the handle. The provider keeps the pending and the committed state of each handle and reports a `Window` when the activated handle, or its `app_id`, changes.

- The windows listed at bind time include the focused one, so unlike the KDE adapter there is no catch-up gap after a daemon restart.
- When no window is activated nothing is reported, as for X11 and KDE; the daemon keeps the previous window until another is focused. A handle with an empty `app_id` is not reported either.
- The protocol carries no PID, so `Window.PID` is 0.
- Titles are put in `Window.Title` only after `SetCaptureTitles(true)` ([ADR 5](0005-privacy-defaults.md)). The protocol sends them regardless, but they never leave the provider's process otherwise.
- `app_id` is the Wayland app ID: usually the desktop-entry ID, but an XWayland client has its `WM_CLASS`. The provider applies the same `apps.WMClassIndex` as the X11 provider, so Firefox is one app whether it runs on X11 or Wayland. An ID not in the index is kept as it is.
- A title change on the focused window is not a new report, as on X11.

**Idle.** `WatchWaylandIdle` (ext-idle-notify-v1), exactly as the KDE provider uses it. It opens its own connection, so focus and idle do not share a read loop.

**Availability.** The socket (`$WAYLAND_DISPLAY` in `$XDG_RUNTIME_DIR`) must answer and the registry must advertise `zwlr_foreign_toplevel_manager_v1`. Mutter and KWin do not advertise it, so the check does not depend on `XDG_CURRENT_DESKTOP`. The provider is listed after GNOME and KDE and before X11.

The wire client in `waylandidle.go` gained arrays (`state`) in `send` and `wlArgs`, and a `globals` helper for the registry listing that idle and focus share. No Wayland library, no cgo, no new dependency. The daemon stays event-driven: it sleeps in a socket read.

## Consequences

- **GNOME and KDE are unaffected**: they are tried first, and neither advertises the protocol.
- Compositors can hide windows from this protocol (a client may be excluded, or the global withheld from sandboxed clients). A restricted global means the provider is unavailable, and the daemon falls through to X11 if there is an X session.
- The protocol is "unstable" but has been stable in practice since 2018, and `ext-foreign-toplevel-list-v1` is its eventual standard replacement. That one has no activated state, so it cannot replace this yet.
- Native Wayland clients may share an `app_id` that is not a desktop-entry ID (some Electron apps, terminals). They are tracked under that name, as `WM_CLASS` is on X11.

## Verification

- `internal/focus/wlroots_test.go`, against the fake compositor in `waylandidle_test.go`: activation switching between two toplevels in either event order, changes held until `done`, titles only with capture on, `closed` of the focused toplevel, `WM_CLASS` mapping, no callbacks after unsubscribe, availability with and without the global, and no compositor.
- The protocol details (opcodes, the `activated` value, the order of events) are written from the protocol XML, not checked against a real compositor.

## Left to check on a real compositor

sway first (headless sway was used for ADR 11), then Hyprland and one of river, labwc and Wayfire:

1. `SCREENTIME_DEBUG=1 screentimed`. At start the focused window is logged; each switch afterwards logs the right app ID (Firefox `firefox` or `org.mozilla.firefox`, foot `foot`).
2. Run an XWayland app and check its app ID maps to the same ID as under X11.
3. With titles off, check every `Window.Title` is empty; turn them on and check they appear.
4. Close the focused window, then focus another. Time between is attributed to the first window until the next report; check that is acceptable.
5. Leave the machine idle past the threshold, as in ADR 11.
