# AppImage packaging

**Status: not implemented.** The `.deb` (see `../deb`) is the supported package for v1.

An AppImage would need to bundle WebKitGTK 4.1 and its dependencies for the Wails UI, and an `AppRun` that installs the `systemd --user` unit and GNOME extension on first launch, since AppImages don't run postinst scripts. That is a sizeable, separate piece of work, and the GNOME extension still has to be installed into the user's own extensions directory either way.
