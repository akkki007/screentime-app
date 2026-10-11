# KWin focus adapter (KDE Plasma, Wayland)

A KWin script that reports the activated window to the tracker daemon. It does no tracking, storage or network access itself. The design is in [ADR 11](../../docs/adr/0011-kde-wayland-adapter.md).

**Status:** implemented and tested against a fake KWin and a fake compositor. The idle half has also run against headless sway. It has not yet been run on a real Plasma session; the checklist is in ADR 11.

## Install

```bash
adapters/kwin-script/install.sh
```

The script installs the package with `kpackagetool6` (or `kpackagetool5`), turns it on in `kwinrc` and reloads KWin. You can also toggle it under **System Settings → Window Management → KWin Scripts → Screentime focus**. To remove it, run `kpackagetool6 --type=KWin/Script --remove screentime`.

## How it works

- **Focus:** on each `windowActivated` (Plasma 6; `clientActivated` on Plasma 5), the script calls `FocusChanged(appId, title, pid)` on `io.github.akkki007.screentime.Daemon`, at `/io/github/akkki007/screentime/KWin`. This goes to the daemon alone and is never broadcast. The daemon only accepts calls from KWin.
- **Titles:** sent as `''` until the daemon's reply says the user turned on "Record window titles".
- **App ID:** the window's `.desktop` file ID (`desktopFileName`), or its window class when it has none.
- **Idle:** not from this script. The daemon asks the compositor directly over the Wayland protocol `ext-idle-notify-v1`.

The daemon selects this adapter (`kde-wayland`) on a Wayland session whose `XDG_CURRENT_DESKTOP` contains `KDE`. `SCREENTIME_FOCUS_PROVIDER=kde-wayland` forces it.

## Debugging

```bash
journalctl --user -f | grep -i screentime          # the daemon, and KWin's script errors
SCREENTIME_DEBUG=1 screentimed                     # logs each focus change
dbus-monitor "interface='io.github.akkki007.screentime.KWin'"   # what the script sends
```

If the daemon starts after you last switched windows, the current window is counted from the next switch.
