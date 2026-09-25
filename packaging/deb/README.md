# .deb packaging

**Status: Phase 5** in the roadmap — not started. Flatpak's sandbox blocks the kind of window/focus tracking this app needs (see `docs/architecture.md#tech-stack`), so `.deb` + AppImage are the primary distribution formats.

Planned contents:

- `control` — package metadata, dependency on WebKitGTK
- postinst/postrm — install the `systemd --user` unit (`packaging/systemd/screentime-daemon.service`) and `.desktop` file, and enable the unit for the installing user
- build script invoking `bun build --compile` for the daemon and desktop binaries
