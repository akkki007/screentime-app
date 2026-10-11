package shell

import (
	"net/url"
	"os/exec"
	"path/filepath"

	"github.com/godbus/dbus/v5"
)

// TrayHostAvailable reports whether something on the session bus is
// listening for tray icons (a StatusNotifier watcher). Stock GNOME has none
// without the AppIndicator extension, and then hiding the window would leave
// the user with nothing to click.
func TrayHostAvailable() bool {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return false
	}
	defer conn.Close()
	var has bool
	err = conn.BusObject().Call("org.freedesktop.DBus.NameHasOwner", 0, "org.kde.StatusNotifierWatcher").Store(&has)
	return err == nil && has
}

// Reveal shows a file in the user's file manager, selected if the file
// manager supports it, and otherwise opens its folder.
func Reveal(path string) {
	if conn, err := dbus.ConnectSessionBus(); err == nil {
		defer conn.Close()
		uri := (&url.URL{Scheme: "file", Path: path}).String()
		call := conn.Object("org.freedesktop.FileManager1", "/org/freedesktop/FileManager1").
			Call("org.freedesktop.FileManager1.ShowItems", 0, []string{uri}, "")
		if call.Err == nil {
			return
		}
	}
	_ = exec.Command("xdg-open", filepath.Dir(path)).Start()
}
