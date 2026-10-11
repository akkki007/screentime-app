#!/usr/bin/env bash
# Installs (or updates) the Screentime KWin script for the current user and
# turns it on. Works on Plasma 6 (kpackagetool6) and Plasma 5 (kpackagetool5).
set -euo pipefail
cd "$(dirname "$0")"

if command -v kpackagetool6 >/dev/null; then
  pkg=kpackagetool6 config=kwriteconfig6 qdbus=$(command -v qdbus6 || command -v qdbus || true)
elif command -v kpackagetool5 >/dev/null; then
  pkg=kpackagetool5 config=kwriteconfig5 qdbus=$(command -v qdbus-qt5 || command -v qdbus || true)
else
  echo "kpackagetool6 or kpackagetool5 not found: is this a KDE Plasma session?" >&2
  exit 1
fi

if "$pkg" --type=KWin/Script --show screentime >/dev/null 2>&1; then
  "$pkg" --type=KWin/Script --upgrade .
else
  "$pkg" --type=KWin/Script --install .
fi
"$config" --file kwinrc --group Plugins --key screentimeEnabled true

if [ -n "$qdbus" ]; then
  # Reloads scripts; unload first so an upgrade takes effect without logging out.
  "$qdbus" org.kde.KWin /Scripting org.kde.kwin.Scripting.unloadScript screentime >/dev/null 2>&1 || true
  "$qdbus" org.kde.KWin /KWin reconfigure
  echo "Installed and enabled. If focus isn't tracked, toggle 'Screentime focus' in System Settings > Window Management > KWin Scripts."
else
  echo "Installed and enabled; log out and back in (qdbus not found to reload KWin)."
fi
