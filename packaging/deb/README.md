# .deb packaging

```bash
packaging/deb/build.sh                  # builds everything, including the UI, into ./dist
SKIP_UI_BUILD=1 packaging/deb/build.sh  # reuse an existing apps/desktop/build
NO_UI=1 packaging/deb/build.sh          # daemon + extension only
```

The package contains:

| Path | What |
| --- | --- |
| `/usr/lib/screentime/daemon` | the tracker, compiled with `bun build --compile` (SQL migrations are embedded) |
| `/usr/lib/screentime/native-host` | the browser native-messaging host, compiled |
| `/usr/lib/systemd/user/screentime-daemon.service` | the user unit, enabled for all users by `postinst` (`systemctl --global enable`) |
| `/usr/share/gnome-shell/extensions/screentime-focus@akkki007.github.io/` | the focus extension |
| `/usr/lib/screentime/ui/Screentime/` and `/usr/bin/screentime` | the dashboard |
| `/usr/lib/mozilla/native-messaging-hosts/…json` | the Firefox host manifest |

Flatpak's sandbox blocks the kind of window/focus tracking this app needs (see `docs/architecture.md#tech-stack`), so `.deb` is the primary format.

## What has been verified

The package builds with normalised file modes. Extracted (not installed) the packaged daemon starts, creates its database, binds its socket and connects to the extension, and the packaged UI launches from a read-only tree without writing shortcuts into your home directory. **It has not been `dpkg -i` installed**, so `postinst`/`prerm` and the global unit enablement are untested.

## Notes

- The UI is unpacked from Electrobun's archive rather than run through its launcher's own installer, which would otherwise install itself per user on first run.
- The user unit avoids `ProtectSystem=`/`PrivateTmp=`: they need unprivileged user namespaces, which Ubuntu 24.04+ restricts. It keeps `RestrictAddressFamilies=AF_UNIX` and `NoNewPrivileges`, verified with a transient `systemd-run` unit.
- The compiled daemon and host are ~80 MB each because they embed the Bun runtime.
- Chromium-family native-messaging manifests need the extension ID, which isn't known until the extension is published, so only Firefox's is shipped; use `install:host` for Chromium.
