#!/usr/bin/env bash
# Checks the GNOME notch in a private headless Shell (scripts/gnome-shell-harness.sh):
# while it is off it must never be on screen, however GNOME shuffles the top bar
# (the overview opening, the monitors changing), with the tracker up or down, and
# it must come back when switched on. Not run in CI: it needs a GNOME Shell.
set -u
H="$(dirname "${BASH_SOURCE[0]}")/gnome-shell-harness.sh"
T="${SCREENTIME_TEST_DIR:-$HOME/.cache/screentime-test}"
N='Main.extensionManager.lookup("screentime-focus@akkki007.github.io").stateObj._notch'
fail=0
state() { "$H" eval "[$N._root.visible, $N._inChrome === true].join(' ')" | sed "s/.*'\"//; s/\"'.*//"; }
rpc() { python3 - "$@" <<'P'
import socket,json,os,sys
s=socket.socket(socket.AF_UNIX); s.connect(os.path.join(os.environ['XDG_RUNTIME_DIR'],'screentime','daemon.sock'))
f=s.makefile('rw'); f.write(json.dumps({"jsonrpc":"2.0","id":1,"method":sys.argv[1],"params":json.loads(sys.argv[2])})+"\n"); f.flush()
while json.loads(f.readline()).get("id")!=1: pass
P
}
expect() { # expect <label> <wanted "visible inChrome">
  got="$(state)"; if [ "$got" = "$2" ]; then echo "ok   $1"; else echo "FAIL $1: got '$got', want '$2'"; fail=1; fi
}
overview() { "$H" eval "Main.overview.show()" >/dev/null; sleep 1.5; "$H" eval "Main.overview.hide()" >/dev/null; sleep 1.5; }
trap '"$H" daemon stop; "$H" down' EXIT

"$H" up; "$H" daemon start; sleep 4
export XDG_RUNTIME_DIR="$T/run/xdg"
rpc settings.set '{"notchEnabled":false}'; sleep 2
expect "off: hidden"                              "false false"
overview; overview; overview
expect "off: hidden after the overview opens and closes" "false false"
"$H" eval "Main.layoutManager._updateVisibility(); Main.layoutManager.emit('monitors-changed')" >/dev/null; sleep 1
expect "off: hidden after GNOME re-evaluates its top bar" "false false"
rpc settings.set '{"notchEnabled":true}'; sleep 3
expect "on: shown"                                "true true"
overview
expect "on: still shown after the overview"       "true true"
rpc settings.set '{"notchEnabled":false}'; sleep 3
overview
expect "off again: hidden, even after the overview" "false false"
"$H" daemon stop; sleep 4
expect "off, tracker gone: still hidden"          "false false"
exit $fail
