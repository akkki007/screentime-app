package focus

import (
	"context"
	"log"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/godbus/dbus/v5"
)

// What the daemon exports for the KWin script (adapters/kwin-script). The
// bus name differs from the GNOME extension's, which owns the bare app ID.
const (
	daemonName    = "io.github.akkki007.screentime.Daemon"
	kwinInterface = "io.github.akkki007.screentime.KWin"
	kwinPath      = dbus.ObjectPath("/io/github/akkki007/screentime/KWin")
	kwinService   = "org.kde.KWin"
)

// KDE reads focus from the Screentime KWin script and idleness from the
// compositor's ext-idle-notify-v1 Wayland protocol (ADR 11).
//
// KWin scripts can call D-Bus methods but cannot export objects or receive
// signals, so the direction is the reverse of GNOME's: the daemon owns a name
// and the script calls FocusChanged on it. That is a method call to one
// destination, never a broadcast (S6). Calls from anyone but the owner of
// org.kde.KWin are refused, and the reply tells the script whether to send
// titles, so they stay empty until the user opts in.
type KDE struct {
	// Connect opens the session bus; tests point it at a private bus.
	Connect func() (*dbus.Conn, error)
	Now     func() int64
	// IdleSocket is the compositor's Wayland socket; "" means
	// $XDG_RUNTIME_DIR/$WAYLAND_DISPLAY.
	IdleSocket string

	once    sync.Once
	conn    *dbus.Conn
	connErr error

	mu            sync.Mutex
	captureTitles bool
	cb            func(Window)

	// godbus runs each incoming call on its own goroutine, so two quick
	// switches can arrive out of order. deliver hands them to cb one at a
	// time, and a call older than the last delivered one (by its serial on
	// KWin's connection) is dropped: a newer window has the focus already.
	deliver    sync.Mutex
	lastSender string
	lastSerial uint32
}

func (k *KDE) ID() string { return "kde-wayland" }

func (k *KDE) bus() (*dbus.Conn, error) {
	k.once.Do(func() {
		connect := k.Connect
		if connect == nil {
			connect = func() (*dbus.Conn, error) { return dbus.ConnectSessionBus() }
		}
		k.conn, k.connErr = connect()
	})
	return k.conn, k.connErr
}

func (k *KDE) now() int64 {
	if k.Now != nil {
		return k.Now()
	}
	return time.Now().UnixMilli()
}

// Available on a KDE Plasma Wayland session with a reachable session bus.
func (k *KDE) Available(context.Context) bool {
	if os.Getenv("XDG_SESSION_TYPE") != "wayland" ||
		!strings.Contains(strings.ToLower(os.Getenv("XDG_CURRENT_DESKTOP")), "kde") {
		return false
	}
	_, err := k.bus()
	return err == nil
}

// SetCaptureTitles is handed to the script in its next FocusChanged reply.
func (k *KDE) SetCaptureTitles(capture bool) {
	k.mu.Lock()
	k.captureTitles = capture
	k.mu.Unlock()
}

// kwinObject is what the script calls. Exported methods are D-Bus methods.
type kwinObject struct{ k *KDE }

// FocusChanged reports the activated window. pid is a string because KWin
// passes JavaScript numbers as doubles. The reply is whether to send titles.
func (o kwinObject) FocusChanged(msg dbus.Message, appID, title, pid string) (bool, *dbus.Error) {
	sender, _ := msg.Headers[dbus.FieldSender].Value().(string)
	return o.k.focusChanged(sender, msg.Serial(), appID, title, pid)
}

func (k *KDE) focusChanged(sender string, serial uint32, appID, title, pid string) (bool, *dbus.Error) {
	conn, err := k.bus()
	if err != nil {
		return false, dbus.MakeFailedError(err)
	}
	// Any client on the bus can call us; only KWin counts.
	var owner string
	if err := conn.BusObject().Call("org.freedesktop.DBus.GetNameOwner", 0, kwinService).Store(&owner); err != nil || sender != owner {
		return false, dbus.NewError("org.freedesktop.DBus.Error.AccessDenied", []any{"only KWin may report focus"})
	}
	k.mu.Lock()
	capture := k.captureTitles
	k.mu.Unlock()
	if !capture {
		title = ""
	}
	if appID == "" {
		return capture, nil
	}

	k.deliver.Lock()
	defer k.deliver.Unlock()
	if sender != k.lastSender {
		k.lastSender, k.lastSerial = sender, 0 // KWin restarted
	}
	if serial <= k.lastSerial {
		return capture, nil
	}
	k.lastSerial = serial
	// k.mu is not held while cb runs: cb takes the daemon's lock, and the
	// daemon unsubscribes while holding it.
	k.mu.Lock()
	cb := k.cb
	k.mu.Unlock()
	if cb != nil {
		n, _ := strconv.ParseUint(pid, 10, 32)
		cb(Window{AppID: appID, Title: title, PID: uint32(n), Ts: k.now()})
	}
	return capture, nil
}

func (k *KDE) OnFocusChange(cb func(Window)) func() {
	conn, err := k.bus()
	if err != nil {
		log.Printf("[kde-wayland] no session bus: %v", err)
		return func() {}
	}
	k.mu.Lock()
	k.cb = cb
	k.mu.Unlock()
	if err := conn.Export(kwinObject{k}, kwinPath, kwinInterface); err != nil {
		log.Printf("[kde-wayland] exporting the focus object: %v", err)
		return func() {}
	}
	reply, err := conn.RequestName(daemonName, dbus.NameFlagDoNotQueue)
	switch {
	case err != nil:
		log.Printf("[kde-wayland] owning %s: %v", daemonName, err)
	case reply != dbus.RequestNameReplyPrimaryOwner && reply != dbus.RequestNameReplyAlreadyOwner:
		log.Printf("[kde-wayland] %s is owned by another process, so focus will not be tracked", daemonName)
	default:
		// The script reports on window activation only, so the window
		// focused right now is picked up at the next switch.
		log.Print("[kde-wayland] waiting for the Screentime KWin script (is it enabled?)")
	}
	return func() {
		k.mu.Lock()
		k.cb = nil
		k.mu.Unlock()
		conn.ReleaseName(daemonName)
		conn.Export(nil, kwinPath, kwinInterface)
	}
}

// OnIdleChange uses ext-idle-notify-v1: KWin's GetSessionIdleTime on
// org.freedesktop.ScreenSaver answers NotSupported on Wayland.
func (k *KDE) OnIdleChange(cb func(bool), thresholdMs int64) func() {
	socket := k.IdleSocket
	if socket == "" {
		socket = WaylandSocket()
	}
	stop, err := WatchWaylandIdle(socket, thresholdMs, cb)
	if err != nil {
		log.Printf("[kde-wayland] idle detection unavailable, so time away will be counted: %v", err)
		return func() {}
	}
	return stop
}
