#!/usr/bin/env bash
# scripts/gnome-shell-harness.sh up|down|eval <js>|shot <png>|daemon start|stop : a private headless GNOME Shell with the repo's
# extension, plus an isolated tracker running on a copy of your data. Never touches the real session or
# data. Needs gnome-shell 45+ (headless mode) and sqlite3 from python. Lives in ~/.cache so a reboot keeps it.
T="${SCREENTIME_TEST_DIR:-$HOME/.cache/screentime-test}"; R=$T/run
REPO="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
case $1 in
up)
  "$0" down; mkdir -p $R/data/gnome-shell/extensions/st-test-helper@local $R/config/glib-2.0/settings $R/xdg; chmod 700 $R/xdg
  cat > $R/config/glib-2.0/settings/keyfile <<'K'
[org/gnome/shell]
disable-user-extensions=false
enabled-extensions=['screentime-focus@akkki007.github.io','st-test-helper@local']
welcome-dialog-last-shown-version='9999'
K
  ln -sfn $REPO/adapters/gnome-extension $R/data/gnome-shell/extensions/screentime-focus@akkki007.github.io
  echo '{"uuid":"st-test-helper@local","name":"Test helper (private test shell only)","description":"Enables unsafe mode.","shell-version":["50"]}' > $R/data/gnome-shell/extensions/st-test-helper@local/metadata.json
  echo "import { Extension } from 'resource:///org/gnome/shell/extensions/extension.js'; export default class H extends Extension { enable() { global.context.unsafe_mode = true; } disable() { global.context.unsafe_mode = false; } }" > $R/data/gnome-shell/extensions/st-test-helper@local/extension.js
  cat > $R/boot.sh <<K
export XDG_RUNTIME_DIR=$R/xdg XDG_DATA_HOME=$R/data XDG_CONFIG_HOME=$R/config GSETTINGS_BACKEND=keyfile
unset WAYLAND_DISPLAY DISPLAY
echo "\$DBUS_SESSION_BUS_ADDRESS" > $R/bus.txt
gnome-shell --headless --virtual-monitor 1280x800 --no-x11 > $R/shell.log 2>&1 &
echo \$! > $R/shell.pid
wait
K
  setsid dbus-run-session -- bash $R/boot.sh >/dev/null 2>&1 < /dev/null &
  sleep 11 ;;
down) for f in shell daemon; do [ -f $R/$f.pid ] && kill $(cat $R/$f.pid) 2>/dev/null; done; sleep 2 ;;
eval) DBUS_SESSION_BUS_ADDRESS=$(cat $R/bus.txt) gdbus call --session -d org.gnome.Shell -o /org/gnome/Shell -m org.gnome.Shell.Eval "$2" ;;
shot) DBUS_SESSION_BUS_ADDRESS=$(cat $R/bus.txt) gdbus call --session -d org.gnome.Shell.Screenshot -o /org/gnome/Shell/Screenshot -m org.gnome.Shell.Screenshot.Screenshot false false "$2" >/dev/null ;;
daemon)
  case $2 in
  start)
    mkdir -p $R/xdg/screentime $R/data/screentime; chmod 700 $R/xdg/screentime $R/data/screentime
    DST=$R/data/screentime/screentime.db python3 - <<'P'
import sqlite3,os
dst=os.environ['DST']
for e in ('','-wal','-shm'):
    try: os.remove(dst+e)
    except FileNotFoundError: pass
s=sqlite3.connect('file:'+os.path.expanduser('~/.local/share/screentime/screentime.db')+'?mode=ro',uri=True); d=sqlite3.connect(dst); s.backup(d); d.close()
P
    chmod 600 $R/data/screentime/screentime.db
    (cd $REPO && GOFLAGS= go build -o $R/screentimed ./cmd/screentimed)
    XDG_RUNTIME_DIR=$R/xdg XDG_DATA_HOME=$R/data SCREENTIME_FOCUS_PROVIDER=none setsid $R/screentimed >$R/daemon.log 2>&1 < /dev/null &
    echo $! > $R/daemon.pid; sleep 2 ;;
  stop) [ -f $R/daemon.pid ] && kill $(cat $R/daemon.pid) 2>/dev/null; sleep 1 ;;
  esac ;;
esac
