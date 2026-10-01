#!/usr/bin/env bash
# Installs the focus-reporter extension for development by symlinking this
# directory into your GNOME extensions folder.
#
#   adapters/gnome-extension/install.sh              symlink and try to enable
#   adapters/gnome-extension/install.sh --uninstall  disable and remove
set -euo pipefail

UUID="screentime-focus@akkki007.github.io"
SRC="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DEST="${XDG_DATA_HOME:-$HOME/.local/share}/gnome-shell/extensions/$UUID"

if [ "${1:-}" = "--uninstall" ]; then
  gnome-extensions disable "$UUID" 2>/dev/null || true
  rm -f "$DEST"
  echo "removed $DEST"
  exit 0
fi

mkdir -p "$(dirname "$DEST")"
ln -sfn "$SRC" "$DEST"
echo "linked $DEST -> $SRC"

if gnome-extensions enable "$UUID" 2>/dev/null; then
  echo "enabled $UUID"
else
  echo "GNOME has not noticed the extension yet."
fi

if [ "${XDG_SESSION_TYPE:-}" = "wayland" ]; then
  echo "On Wayland, log out and back in once so GNOME Shell loads it, then run:"
  echo "  gnome-extensions enable $UUID"
else
  echo "On X11, press Alt+F2, type r, press Enter to reload the shell."
fi
