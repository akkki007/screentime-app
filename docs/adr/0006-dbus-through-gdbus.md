# 6. D-Bus through `gdbus`, not a JavaScript library

Date: 2026-10-01

## Status

Accepted. Supersedes the `dbus-next` choice in `docs/architecture.md`.

## Context

The Phase 1 exit criterion is a daemon under 60 MB RSS. After the tracker was built it sat at about 62 MB. A bare Bun process is ~14 MB; measuring imports showed `dbus-next` alone accounting for roughly 25 MB (≈14 MB to load, ≈13 MB more once connected and introspecting), with no way to trim it. The architecture doc already named shelling out to `gdbus` as the fallback.

## Decision

Reach D-Bus through the `gdbus` command-line tool (part of GLib, present on GNOME systems), via the injectable `Runner` that the X11 adapter already used:

- **Focus:** one long-running `gdbus monitor` on the extension's name streams `FocusChanged` and also reports the name appearing or disappearing; each time it appears the daemon calls `GetFocus` to catch up. GVariant text is parsed by a small purpose-built parser (`gvariant.ts`) that handles both quote styles GLib uses, escapes and Unicode.
- **Idle:** poll `org.gnome.Mutter.IdleMonitor.GetIdletime` every 3 s. An idle *watch* cannot be used because it is tied to the caller's connection and a one-shot `gdbus call` closes it.
- **Notifications:** one `gdbus call` to `org.freedesktop.Notifications`, with user-influenced text passed through `quoteString` as GVariant literals (no shell is involved).

## Consequences

- Daemon memory fell from ~62 MB to ~43 MB; the `dbus-next` dependency is gone.
- Idle detection is up to 3 s late, so a session can include up to 3 s of idleness at the end of each idle period. Accepted for the saving.
- The daemon needs `gdbus` (the provider reports itself unavailable without it) and spawns a `gdbus` process every 3 s for idle. A persistent connection can come back if a lighter library appears.
- Everything is unit-tested against scripted tool output, and was verified live against GNOME 50.1 with a stand-in extension.
