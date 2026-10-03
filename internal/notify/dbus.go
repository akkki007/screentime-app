package notify

import (
	"log"
	"sync"

	"github.com/godbus/dbus/v5"
)

const appIcon = "io.github.akkki007.screentime"

// DBus shows notifications through org.freedesktop.Notifications, which every
// desktop implements. The connection is opened on first use and reopened
// after a failure.
type DBus struct {
	// Connect opens the session bus; tests point it at a private bus.
	Connect func() (*dbus.Conn, error)

	mu   sync.Mutex
	conn *dbus.Conn
}

func (n *DBus) Notify(title, body string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.conn == nil {
		connect := n.Connect
		if connect == nil {
			connect = func() (*dbus.Conn, error) { return dbus.ConnectSessionBus() }
		}
		conn, err := connect()
		if err != nil {
			log.Printf("[notifier] no session bus: %v", err)
			return
		}
		n.conn = conn
	}
	// Notify(app_name, replaces_id, app_icon, summary, body, actions, hints, expire_timeout)
	call := n.conn.Object("org.freedesktop.Notifications", "/org/freedesktop/Notifications").Call(
		"org.freedesktop.Notifications.Notify", 0,
		"Screentime", uint32(0), appIcon, title, body, []string{}, map[string]dbus.Variant{}, int32(8000),
	)
	// A missing notification daemon must never take tracking down.
	if call.Err != nil {
		log.Printf("[notifier] could not send a notification (is a notification daemon running?): %v", call.Err)
		n.conn.Close()
		n.conn = nil
	}
}
