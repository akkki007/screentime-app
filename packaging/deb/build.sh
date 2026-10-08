#!/usr/bin/env bash
# Builds screentime_<version>_<arch>.deb into ./dist (or the directory given as $1).
#
#   packaging/deb/build.sh                  build everything, including the UI
#   SKIP_UI_BUILD=1 packaging/deb/build.sh  reuse an existing frontend/dist
#   NO_UI=1 packaging/deb/build.sh          daemon + extension only (no UI)
#
# Needs: go, dpkg-deb, and (for the UI) bun, a C compiler, and the
# libwebkit2gtk-4.1-dev and libgtk-3-dev packages.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
OUT="${1:-$ROOT/dist}"
cd "$ROOT"

VERSION="$(sed -n 's/^ *"version": *"\([^"]*\)".*/\1/p' package.json | head -n1)"
ARCH="$(dpkg --print-architecture)"
EXT_UUID="screentime-focus@akkki007.github.io"
HOST_NAME="io.github.akkki007.screentime"
STAGE="$(mktemp -d)"
trap 'rm -rf "$STAGE"' EXIT

echo "==> daemon (screentimed)"
install -d "$STAGE/usr/lib/screentime"
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o "$STAGE/usr/lib/screentime/screentimed" ./cmd/screentimed

echo "==> browser native-messaging host (screentimed native-host)"
# Browsers launch the manifest's path with their own arguments, so it is a
# symlink to screentimed, which recognises the link's name.
ln -s screentimed "$STAGE/usr/lib/screentime/screentime-native-host"
install -d "$STAGE/usr/lib/mozilla/native-messaging-hosts"
cat > "$STAGE/usr/lib/mozilla/native-messaging-hosts/$HOST_NAME.json" <<JSON
{
  "name": "$HOST_NAME",
  "description": "Screentime: forwards the active tab domain to the local daemon",
  "path": "/usr/lib/screentime/screentime-native-host",
  "type": "stdio",
  "allowed_extensions": ["screentime@akkki007.github.io"]
}
JSON

echo "==> systemd user unit"
install -Dm644 packaging/systemd/screentime-daemon.service \
  "$STAGE/usr/lib/systemd/user/screentime-daemon.service"

echo "==> GNOME Shell extension"
EXT_DIR="$STAGE/usr/share/gnome-shell/extensions/$EXT_UUID"
install -Dm644 adapters/gnome-extension/extension.js "$EXT_DIR/extension.js"
install -Dm644 adapters/gnome-extension/metadata.json "$EXT_DIR/metadata.json"

echo "==> desktop entry and icon"
install -Dm644 "packaging/$HOST_NAME.desktop" "$STAGE/usr/share/applications/$HOST_NAME.desktop"
install -Dm644 cmd/screentime/icons/tray.png \
  "$STAGE/usr/share/icons/hicolor/64x64/apps/$HOST_NAME.png"

if [ -z "${NO_UI:-}" ]; then
  echo "==> UI (screentime)"
  if [ -z "${SKIP_UI_BUILD:-}" ]; then
    bun run --cwd frontend build
  fi
  # `production` turns off Wails' development hooks, among them the
  # FRONTEND_DEVSERVER_URL override (S4): a packaged app only ever shows the
  # build embedded in the binary.
  install -d "$STAGE/usr/bin"
  CGO_ENABLED=1 go build -tags gtk3,production -trimpath -ldflags="-s -w" \
    -o "$STAGE/usr/bin/screentime" ./cmd/screentime
  DEPENDS="libwebkit2gtk-4.1-0, libgtk-3-0"
else
  DEPENDS="libc6"
fi

echo "==> control files"
install -d "$STAGE/DEBIAN"
SIZE_KB="$(du -sk "$STAGE" | cut -f1)"
cat > "$STAGE/DEBIAN/control" <<CONTROL
Package: screentime
Version: $VERSION-1
Section: utils
Priority: optional
Architecture: $ARCH
Depends: $DEPENDS
Recommends: gnome-shell-extension-appindicator
Suggests: firefox
Installed-Size: $SIZE_KB
Maintainer: Screentime contributors <noreply@users.noreply.github.com>
Homepage: https://github.com/akkki007/screentime-app
Description: Local-first screentime and digital wellbeing tracker
 Tracks app usage on your desktop, shows where your time goes, nudges you to
 take breaks and enforces daily limits. Everything stays on your computer: the
 tracker never uses the network. Includes the tracker daemon, a GNOME Shell
 extension that reports the focused window, and the dashboard.
CONTROL

cat > "$STAGE/DEBIAN/postinst" <<'POSTINST'
#!/bin/sh
set -e
if [ "$1" = "configure" ]; then
  # Enable the tracker for every user's session (the standard way to enable a user unit).
  if command -v systemctl >/dev/null 2>&1; then
    systemctl --global enable screentime-daemon.service >/dev/null 2>&1 || true
  fi
  if command -v gtk-update-icon-cache >/dev/null 2>&1; then
    gtk-update-icon-cache -q -t -f /usr/share/icons/hicolor 2>/dev/null || true
  fi
  cat <<'MSG'

Screentime is installed. To finish setting up, as your own user:
  systemctl --user start screentime-daemon
  gnome-extensions enable screentime-focus@akkki007.github.io
On GNOME/Wayland, log out and back in once so the Shell loads the extension.

MSG
fi
exit 0
POSTINST

cat > "$STAGE/DEBIAN/prerm" <<'PRERM'
#!/bin/sh
set -e
if [ "$1" = "remove" ] || [ "$1" = "purge" ]; then
  if command -v systemctl >/dev/null 2>&1; then
    systemctl --global disable screentime-daemon.service >/dev/null 2>&1 || true
  fi
fi
exit 0
PRERM
chmod 755 "$STAGE/DEBIAN/postinst" "$STAGE/DEBIAN/prerm"

# Normalise modes: nothing group/world-writable, and the root readable. (mktemp
# makes the staging root 0700 and the umask can leave files group-writable.)
chmod -R u+rwX,go+rX,go-w "$STAGE"
chmod 755 "$STAGE"

mkdir -p "$OUT"
DEB="$OUT/screentime_${VERSION}-1_${ARCH}.deb"
dpkg-deb --build --root-owner-group "$STAGE" "$DEB" >/dev/null
echo "==> built $DEB ($(du -h "$DEB" | cut -f1))"
