#!/usr/bin/env bash
# Builds screentimed from this checkout and runs it as a systemd *user*
# service, so tracking survives closing the terminal and starts at login.
#
#   packaging/systemd/install-dev.sh              build, install + start
#   packaging/systemd/install-dev.sh --dry-run    show what would be written
#   packaging/systemd/install-dev.sh --uninstall  stop and remove it
#
# Needs Go (see go.mod). Re-run it after pulling to rebuild and restart.
set -euo pipefail

REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
UNIT_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user"
UNIT="$UNIT_DIR/screentime-daemon.service"
MODE="install"
for arg in "$@"; do
  case "$arg" in
    --dry-run) MODE="dry-run" ;;
    --uninstall) MODE="uninstall" ;;
    -h|--help) sed -n '2,8p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) echo "unknown option: $arg" >&2; exit 2 ;;
  esac
done

if [ "$MODE" = "uninstall" ]; then
  systemctl --user disable --now screentime-daemon.service 2>/dev/null || true
  rm -f "$UNIT"
  systemctl --user daemon-reload
  echo "removed $UNIT"
  exit 0
fi

BIN="$REPO/bin/screentimed"

# systemd does not use your shell's PATH, so everything is absolute.
CONTENT="[Unit]
Description=Screentime tracker daemon (development checkout)
After=graphical-session.target
PartOf=graphical-session.target

[Service]
Type=simple
ExecStart=$BIN
Restart=on-failure
RestartSec=2
UMask=0077
RuntimeDirectory=screentime
RuntimeDirectoryMode=0700
RestrictAddressFamilies=AF_UNIX
NoNewPrivileges=true
MemoryDenyWriteExecute=true

[Install]
WantedBy=graphical-session.target
"

if [ "$MODE" = "dry-run" ]; then
  echo "# would write $UNIT"
  printf '%s' "$CONTENT"
  exit 0
fi

if ! command -v go >/dev/null 2>&1; then
  echo "go not found on PATH. Install it from https://go.dev/dl/ first." >&2
  exit 1
fi
(cd "$REPO" && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o "$BIN" ./cmd/screentimed)
echo "built $BIN"

mkdir -p "$UNIT_DIR"
printf '%s' "$CONTENT" > "$UNIT"
systemctl --user daemon-reload
systemctl --user enable screentime-daemon.service
systemctl --user restart screentime-daemon.service
echo "installed $UNIT"
echo "logs:   journalctl --user -u screentime-daemon -f"
echo "remove: $0 --uninstall"
