# AppImage packaging

**Status: Phase 5** in the roadmap — not started.

Planned contents:

- AppDir layout bundling the compiled daemon + desktop binaries and WebKitGTK dependencies
- `AppRun` entry point that also installs/refreshes the `systemd --user` unit on first launch, since AppImages don't run postinst scripts
- Built via `appimagetool` in CI on tagged releases
