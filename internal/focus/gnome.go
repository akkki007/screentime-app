package focus

import (
	"context"
	"errors"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
)

// The Screentime Shell extension (adapters/gnome-extension).
const (
	extensionName  = "io.github.akkki007.screentime"
	focusInterface = extensionName + ".Focus"
	focusPath      = dbus.ObjectPath("/io/github/akkki007/screentime/Focus")

	idleService   = "org.gnome.Mutter.IdleMonitor"
	idleInterface = "org.gnome.Mutter.IdleMonitor"
	idlePath      = dbus.ObjectPath("/org/gnome/Mutter/IdleMonitor/Core")
)

// Gnome reads focus from the Screentime Shell extension and idleness from
// Mutter's IdleMonitor, over one persistent session-bus connection.
//
// Unlike the Bun daemon's gdbus tool (ADR 6), the connection can subscribe
// (S6, issue #3): the extension then sends FocusChanged to this connection
// only, with titles only while SetCaptureTitles(true) is in effect, and
// signals from anyone but the extension's current owner are dropped. Idle
// uses real Mutter watches instead of polling.
type Gnome struct {
	// Connect opens the session bus; tests point it at a private bus.
	Connect func() (*dbus.Conn, error)
	Now     func() int64

	once    sync.Once
	conn    *dbus.Conn
	connErr error

	mu            sync.Mutex
	owner         string // the extension's unique name, "" when absent
	captureTitles bool
}

func (g *Gnome) ID() string { return "gnome-wayland" }

func (g *Gnome) bus() (*dbus.Conn, error) {
	g.once.Do(func() {
		connect := g.Connect
		if connect == nil {
			connect = func() (*dbus.Conn, error) { return dbus.ConnectSessionBus() }
		}
		g.conn, g.connErr = connect()
	})
	return g.conn, g.connErr
}

func (g *Gnome) now() int64 {
	if g.Now != nil {
		return g.Now()
	}
	return time.Now().UnixMilli()
}

// Available on a GNOME Wayland session with a reachable session bus.
func (g *Gnome) Available(context.Context) bool {
	if os.Getenv("XDG_SESSION_TYPE") != "wayland" ||
		!strings.Contains(strings.ToLower(os.Getenv("XDG_CURRENT_DESKTOP")), "gnome") {
		return false
	}
	_, err := g.bus()
	return err == nil
}

// SetCaptureTitles passes the user's "Record window titles" setting to the
// extension, which leaves titles out until it is on.
func (g *Gnome) SetCaptureTitles(capture bool) {
	g.mu.Lock()
	g.captureTitles = capture
	owner := g.owner
	g.mu.Unlock()
	if owner != "" {
		g.callExtension(owner, "SetCaptureTitles", capture)
	}
}

func (g *Gnome) callExtension(owner, method string, args ...any) *dbus.Call {
	conn, err := g.bus()
	if err != nil {
		return &dbus.Call{Err: err}
	}
	return conn.Object(owner, focusPath).Call(focusInterface+"."+method, 0, args...)
}

func (g *Gnome) OnFocusChange(cb func(Window)) func() {
	conn, err := g.bus()
	if err != nil {
		log.Printf("[gnome-wayland] no session bus: %v", err)
		return func() {}
	}
	signals := make(chan *dbus.Signal, 64)
	conn.Signal(signals)
	matches := [][]dbus.MatchOption{
		{dbus.WithMatchSender("org.freedesktop.DBus"), dbus.WithMatchMember("NameOwnerChanged"), dbus.WithMatchArg(0, extensionName)},
		// Only needed for extension builds that still broadcast.
		{dbus.WithMatchInterface(focusInterface), dbus.WithMatchMember("FocusChanged"), dbus.WithMatchObjectPath(focusPath)},
	}
	for _, m := range matches {
		if err := conn.AddMatchSignal(m...); err != nil {
			log.Printf("[gnome-wayland] adding match: %v", err)
		}
	}

	done := make(chan struct{})
	go func() {
		var owner string
		if err := conn.BusObject().Call("org.freedesktop.DBus.GetNameOwner", 0, extensionName).Store(&owner); err == nil {
			g.attach(owner, cb)
		} else {
			waiting()
		}
		for {
			select {
			case <-done:
				return
			case sig, ok := <-signals:
				if !ok {
					return
				}
				g.handleSignal(sig, cb)
			}
		}
	}()
	return func() {
		close(done)
		conn.RemoveSignal(signals)
		for _, m := range matches {
			conn.RemoveMatchSignal(m...)
		}
	}
}

func waiting() {
	log.Print("[gnome-wayland] waiting for the Screentime Shell extension (is it enabled? GNOME also deactivates it while the screen is locked)")
}

func (g *Gnome) handleSignal(sig *dbus.Signal, cb func(Window)) {
	switch sig.Name {
	case "org.freedesktop.DBus.NameOwnerChanged":
		if len(sig.Body) != 3 {
			return
		}
		name, _ := sig.Body[0].(string)
		newOwner, _ := sig.Body[2].(string)
		if name != extensionName {
			return
		}
		if newOwner == "" {
			g.mu.Lock()
			g.owner = ""
			g.mu.Unlock()
			waiting()
			return
		}
		// The extension (re)appeared: it loads alongside login and GNOME
		// turns it off on the lock screen.
		g.attach(newOwner, cb)
	case focusInterface + ".FocusChanged":
		g.mu.Lock()
		owner := g.owner
		g.mu.Unlock()
		// Anyone can send a signal with this name; only the extension counts.
		if owner == "" || sig.Sender != owner {
			return
		}
		if w, ok := g.window(sig.Body); ok {
			cb(w)
		}
	}
}

// attach subscribes to a newly seen extension, sends the title setting and
// reports the window focused right now.
func (g *Gnome) attach(owner string, cb func(Window)) {
	g.mu.Lock()
	g.owner = owner
	capture := g.captureTitles
	g.mu.Unlock()

	if err := g.callExtension(owner, "Subscribe").Err; err != nil {
		var dbusErr dbus.Error
		switch {
		case errors.As(err, &dbusErr) && dbusErr.Name == "org.freedesktop.DBus.Error.UnknownMethod":
			log.Print("[gnome-wayland] the Shell extension is out of date and broadcasts window titles; update it")
		case errors.As(err, &dbusErr) && dbusErr.Name == "org.freedesktop.DBus.Error.AccessDenied":
			log.Print("[gnome-wayland] another process is subscribed to the Shell extension; focus may not be tracked")
		default:
			log.Printf("[gnome-wayland] subscribing: %v", err)
		}
	} else if err := g.callExtension(owner, "SetCaptureTitles", capture).Err; err != nil {
		log.Printf("[gnome-wayland] setting title capture: %v", err)
	}

	var appID, title string
	var pid uint32
	if err := g.callExtension(owner, "GetFocus").Store(&appID, &title, &pid); err == nil {
		if w, ok := g.window([]any{appID, title, pid}); ok {
			cb(w)
		}
	}
}

func (g *Gnome) window(body []any) (Window, bool) {
	if len(body) != 3 {
		return Window{}, false
	}
	appID, _ := body[0].(string)
	title, _ := body[1].(string)
	pid, _ := body[2].(uint32)
	if appID == "" {
		return Window{}, false
	}
	return Window{AppID: appID, Title: title, PID: pid, Ts: g.now()}, true
}

// OnIdleChange watches Mutter: an idle watch fires when there has been no
// input for thresholdMs, then a one-shot user-active watch fires on the next
// input. The watches belong to this connection, so they need no polling.
func (g *Gnome) OnIdleChange(cb func(bool), thresholdMs int64) func() {
	conn, err := g.bus()
	if err != nil {
		return func() {}
	}
	monitor := conn.Object(idleService, idlePath)
	signals := make(chan *dbus.Signal, 16)
	conn.Signal(signals)
	match := []dbus.MatchOption{dbus.WithMatchInterface(idleInterface), dbus.WithMatchMember("WatchFired"), dbus.WithMatchObjectPath(idlePath)}
	if err := conn.AddMatchSignal(match...); err != nil {
		log.Printf("[gnome-wayland] adding idle match: %v", err)
	}

	var idleWatch uint32
	if err := monitor.Call(idleInterface+".AddIdleWatch", 0, uint64(thresholdMs)).Store(&idleWatch); err != nil {
		log.Printf("[gnome-wayland] idle detection unavailable, so time away will be counted: %v", err)
	}

	done := make(chan struct{})
	go func() {
		var activeWatch uint32
		goneIdle := func() {
			cb(true)
			if err := monitor.Call(idleInterface+".AddUserActiveWatch", 0).Store(&activeWatch); err != nil {
				log.Printf("[gnome-wayland] watching for activity: %v", err)
			}
		}
		// Already idle when subscribing (e.g. the threshold just changed)?
		var idleMs uint64
		if err := monitor.Call(idleInterface+".GetIdletime", 0).Store(&idleMs); err == nil && int64(idleMs) >= thresholdMs {
			goneIdle()
		}
		for {
			select {
			case <-done:
				return
			case sig, ok := <-signals:
				if !ok {
					return
				}
				if sig.Name != idleInterface+".WatchFired" || len(sig.Body) != 1 {
					continue
				}
				id, _ := sig.Body[0].(uint32)
				switch {
				case idleWatch != 0 && id == idleWatch:
					goneIdle()
				case activeWatch != 0 && id == activeWatch:
					activeWatch = 0 // user-active watches fire once
					cb(false)
				}
			}
		}
	}()
	return func() {
		close(done)
		conn.RemoveSignal(signals)
		conn.RemoveMatchSignal(match...)
		if idleWatch != 0 {
			monitor.Call(idleInterface+".RemoveWatch", 0, idleWatch)
		}
	}
}
