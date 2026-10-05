package focus

import (
	"bytes"
	"context"
	"log"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/screensaver"
	"github.com/jezek/xgb/xproto"
)

// X11 reads focus from the root window's _NET_ACTIVE_WINDOW and idleness
// from the MIT-SCREEN-SAVER extension, speaking the X protocol directly
// (xgb) instead of running xprop and xprintidle like the Bun daemon.
type X11 struct {
	// Display is the X display to use; "" means $DISPLAY.
	Display string
	Now     func() int64
	// Index maps lowercased WM_CLASS to desktop-entry IDs, so an X11 window
	// gets the same app ID Wayland reports (apps.WMClassIndex).
	Index func() map[string]string
	// IdlePoll is how often idle time is checked (default 5 s).
	IdlePoll time.Duration

	indexOnce sync.Once
	index     map[string]string
}

func (x *X11) ID() string { return "x11" }

func (x *X11) connect() (*xgb.Conn, error) { return xgb.NewConnDisplay(x.Display) }

// Available on an X11 session whose display accepts a connection.
func (x *X11) Available(context.Context) bool {
	if os.Getenv("XDG_SESSION_TYPE") != "x11" || os.Getenv("DISPLAY") == "" {
		return false
	}
	conn, err := x.connect()
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

func (x *X11) now() int64 {
	if x.Now != nil {
		return x.Now()
	}
	return time.Now().UnixMilli()
}

type x11Atoms struct{ activeWindow, wmName, wmPID, utf8 xproto.Atom }

func internAtoms(conn *xgb.Conn) (x11Atoms, error) {
	var a x11Atoms
	for name, dst := range map[string]*xproto.Atom{
		"_NET_ACTIVE_WINDOW": &a.activeWindow,
		"_NET_WM_NAME":       &a.wmName,
		"_NET_WM_PID":        &a.wmPID,
		"UTF8_STRING":        &a.utf8,
	} {
		reply, err := xproto.InternAtom(conn, false, uint16(len(name)), name).Reply()
		if err != nil {
			return a, err
		}
		*dst = reply.Atom
	}
	return a, nil
}

// OnFocusChange reports the active window at once, then every change, in
// order. Each subscription has its own connection, closed on unsubscribe.
func (x *X11) OnFocusChange(cb func(Window)) func() {
	conn, err := x.connect()
	if err != nil {
		log.Printf("[x11] cannot connect to the display: %v", err)
		return func() {}
	}
	atoms, err := internAtoms(conn)
	if err != nil {
		log.Printf("[x11] interning atoms: %v", err)
		conn.Close()
		return func() {}
	}
	root := xproto.Setup(conn).DefaultScreen(conn).Root
	if err := xproto.ChangeWindowAttributesChecked(conn, root, xproto.CwEventMask,
		[]uint32{xproto.EventMaskPropertyChange}).Check(); err != nil {
		log.Printf("[x11] watching the root window: %v", err)
		conn.Close()
		return func() {}
	}

	var stopped atomic.Bool
	last := xproto.Window(1<<32 - 1) // not a real window: the first read reports
	report := func() {
		// Several notifications can arrive before we read the property, and
		// each read sees its latest value; report each window once.
		win := activeWindow(conn, root, atoms)
		if win == last {
			return
		}
		last = win
		if w, ok := x.describe(conn, atoms, win); ok && !stopped.Load() {
			cb(w)
		}
	}
	go func() {
		report() // the property only notifies on change
		for {
			ev, err := conn.WaitForEvent()
			if ev == nil && err == nil {
				return // connection closed
			}
			if p, ok := ev.(xproto.PropertyNotifyEvent); ok && p.Window == root && p.Atom == atoms.activeWindow {
				report()
			}
		}
	}()
	return func() {
		stopped.Store(true)
		conn.Close()
	}
}

func activeWindow(conn *xgb.Conn, root xproto.Window, atoms x11Atoms) xproto.Window {
	reply, err := xproto.GetProperty(conn, false, root, atoms.activeWindow, xproto.AtomWindow, 0, 1).Reply()
	if err != nil || reply.Format != 32 || len(reply.Value) < 4 {
		return 0
	}
	return xproto.Window(xgb.Get32(reply.Value))
}

// describe reads a window's class, PID and title; ok is false for no window
// or one without WM_CLASS.
func (x *X11) describe(conn *xgb.Conn, atoms x11Atoms, win xproto.Window) (Window, bool) {
	if win == 0 {
		return Window{}, false
	}
	class := ParseWMClass(property(conn, win, xproto.AtomWmClass, xproto.AtomString))
	if class == "" {
		return Window{}, false
	}
	w := Window{AppID: x.appIDFor(class), Title: string(property(conn, win, atoms.wmName, atoms.utf8)), Ts: x.now()}
	if pid := property(conn, win, atoms.wmPID, xproto.AtomCardinal); len(pid) >= 4 {
		w.PID = xgb.Get32(pid)
	}
	return w, true
}

func property(conn *xgb.Conn, win xproto.Window, prop, typ xproto.Atom) []byte {
	reply, err := xproto.GetProperty(conn, false, win, prop, typ, 0, 1024).Reply()
	if err != nil {
		return nil
	}
	return reply.Value
}

// ParseWMClass returns the class (second string) of a WM_CLASS value
// ("instance\x00Class\x00"), or the instance when the class is empty.
func ParseWMClass(value []byte) string {
	parts := bytes.Split(bytes.TrimRight(value, "\x00"), []byte{0})
	if len(parts) >= 2 && len(parts[1]) > 0 {
		return string(parts[1])
	}
	return string(parts[0])
}

// appIDFor prefers the desktop-entry ID, as Wayland reports; otherwise the
// WM_CLASS itself.
func (x *X11) appIDFor(class string) string {
	x.indexOnce.Do(func() {
		if x.Index != nil {
			x.index = x.Index()
		}
	})
	if id, ok := x.index[strings.ToLower(class)]; ok {
		return id
	}
	return class
}

// initScreenSaver registers MIT-SCREEN-SAVER on this connection only.
// screensaver.Init would also write xgb's package-level event tables without
// a lock, racing with every other connection's event loop (a fatal
// concurrent map access); idle polling needs no events, just the opcode.
func initScreenSaver(conn *xgb.Conn) error {
	const name = "MIT-SCREEN-SAVER"
	reply, err := xproto.QueryExtension(conn, uint16(len(name)), name).Reply()
	if err != nil {
		return err
	}
	if !reply.Present {
		return xgb.Errorf("no %s extension", name)
	}
	conn.ExtLock.Lock()
	conn.Extensions[name] = reply.MajorOpcode
	conn.ExtLock.Unlock()
	return nil
}

// OnIdleChange polls the time since the last input and reports crossings of
// thresholdMs. Without MIT-SCREEN-SAVER there is no idle detection, and time
// away is counted; that is logged rather than fatal.
func (x *X11) OnIdleChange(cb func(bool), thresholdMs int64) func() {
	conn, err := x.connect()
	if err != nil {
		return func() {}
	}
	if err := initScreenSaver(conn); err != nil {
		log.Print("[x11] the X server lacks MIT-SCREEN-SAVER, so idle time cannot be detected and time away will be counted")
		conn.Close()
		return func() {}
	}
	every := x.IdlePoll
	if every == 0 {
		every = 5 * time.Second
	}
	done := make(chan struct{})
	go pollIdle(done, every, thresholdMs, func() (int64, error) { return idleMs(conn) }, cb)
	return func() {
		close(done)
		conn.Close()
	}
}

// idleMs is the time since the last keyboard or pointer input.
func idleMs(conn *xgb.Conn) (int64, error) {
	root := xproto.Drawable(xproto.Setup(conn).DefaultScreen(conn).Root)
	info, err := screensaver.QueryInfo(conn, root).Reply()
	if err != nil {
		return 0, err
	}
	return int64(info.MsSinceUserInput), nil
}

// pollIdle calls query every interval until done is closed, and reports each
// crossing of thresholdMs (not every poll). The first failure is logged.
func pollIdle(done <-chan struct{}, every time.Duration, thresholdMs int64, query func() (int64, error), cb func(bool)) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	idle, warned := false, false
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			ms, err := query()
			if err != nil {
				if !warned {
					log.Printf("[x11] reading idle time: %v", err)
					warned = true
				}
				continue
			}
			if now := ms >= thresholdMs; now != idle {
				idle = now
				cb(idle)
			}
		}
	}
}
