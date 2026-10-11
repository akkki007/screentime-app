# .deb packaging

```bash
packaging/deb/build.sh                  # builds everything, including the UI, into ./dist
SKIP_UI_BUILD=1 packaging/deb/build.sh  # reuse an existing frontend/dist
NO_UI=1 packaging/deb/build.sh          # daemon + extension only (3 MB)
```

The package contains:

| Path | What |
| --- | --- |
| `/usr/lib/screentime/screentimed` | the tracker: one static Go binary (migrations are embedded) |
| `/usr/lib/screentime/screentime-native-host` | the browser native-messaging host: a symlink to `screentimed`, which runs as the host when started under that name |
| `/usr/lib/systemd/user/screentime-daemon.service` | the user unit, enabled for all users by `postinst` (`systemctl --global enable`) |
| `/usr/share/gnome-shell/extensions/screentime-focus@akkki007.github.io/` | the focus extension |
| `/usr/share/kwin/scripts/screentime/` | the KWin script (KDE Plasma) |
| `/usr/bin/screentime` | the desktop app: one Go binary with the Svelte UI embedded, built with `-tags gtk3,production`. Needs `libwebkit2gtk-4.1-0` and `libgtk-3-0` |
| `/usr/lib/mozilla/native-messaging-hosts/…json` | the Firefox host manifest |

Flatpak's sandbox blocks the kind of window/focus tracking this app needs (see `docs/architecture.md#tech-stack`), so `.deb` is the primary format.

## What has been verified

The package builds with normalised file modes. Extracted (not installed), the packaged daemon starts, creates its database and binds its socket, and the app builds with its dependencies satisfied by the package's `Depends`. **Installation on a real desktop is not yet verified**: `postinst`/`prerm` and the global unit enablement are untested outside a sandbox.

## Notes

- The user unit avoids `ProtectSystem=`/`PrivateTmp=`: they need unprivileged user namespaces, which Ubuntu 24.04+ restricts. It keeps `RestrictAddressFamilies=AF_UNIX`, `NoNewPrivileges`, `UMask=0077` and, now that nothing JITs, `MemoryDenyWriteExecute`.
- Size: the Bun/Electrobun package was 30 MB (86 MB installed). The Go package is 6.7 MB (19 MB installed); the daemon alone is 3.2 MB.
- Chromium-family native-messaging manifests need the extension ID, which isn't known until the extension is published, so only Firefox's is shipped; for Chromium-family browsers write a manifest whose `path` is `/usr/lib/screentime/screentime-native-host` (see the extension README).
