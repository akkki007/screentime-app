//go:build linux

package sandbox

import (
	"os"
	"strings"
	"syscall"

	"github.com/godbus/dbus/v5"
)

// Detect returns the systemd user unit this process runs as, or nil when it
// was not started by the user's service manager (go run, a terminal, tests).
func Detect() *Unit {
	if os.Getenv("INVOCATION_ID") == "" {
		return nil
	}
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return nil
	}
	defer conn.Close()
	manager := conn.Object("org.freedesktop.systemd1", "/org/freedesktop/systemd1")
	var path dbus.ObjectPath
	if err := manager.Call("org.freedesktop.systemd1.Manager.GetUnitByPID", 0, uint32(os.Getpid())).Store(&path); err != nil {
		return nil
	}
	v, err := conn.Object("org.freedesktop.systemd1", path).GetProperty("org.freedesktop.systemd1.Unit.Id")
	if err != nil {
		return nil
	}
	name, _ := v.Value().(string)
	if !strings.HasSuffix(name, ".service") {
		return nil
	}
	dir, err := DropInDir(name)
	if err != nil {
		return nil
	}
	return &Unit{Dir: dir, Restart: func() error { return restart(name) }}
}

func restart(name string) error {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return err
	}
	defer conn.Close()
	manager := conn.Object("org.freedesktop.systemd1", "/org/freedesktop/systemd1")
	if err := manager.Call("org.freedesktop.systemd1.Manager.Reload", 0).Err; err != nil {
		return err
	}
	// The manager stops this process with SIGTERM, the normal shutdown path.
	return manager.Call("org.freedesktop.systemd1.Manager.RestartUnit", 0, name, "replace").Err
}

// NetworkAllowed reports whether this process may open an IPv4 socket, which
// RestrictAddressFamilies=AF_UNIX forbids.
func NetworkAllowed() bool {
	fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_STREAM, 0)
	if err != nil {
		return false
	}
	syscall.Close(fd)
	return true
}
