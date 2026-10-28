//go:build linux

package focus

import (
	"context"
	"errors"
	"log"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// zwlr_foreign_toplevel_management_unstable_v1, the protocol sway, Hyprland,
// river, labwc, Wayfire and other wlroots compositors use to list windows to
// taskbars. Like waylandidle.go it speaks only the messages it needs.
const (
	wlrManagerInterface = "zwlr_foreign_toplevel_manager_v1"

	// zwlr_foreign_toplevel_manager_v1
	evManagerToplevel = 0
	evManagerFinished = 1
	// zwlr_foreign_toplevel_handle_v1
	evHandleTitle   = 0
	evHandleAppID   = 1
	evHandleState   = 4
	evHandleDone    = 5
	evHandleClosed  = 6
	opHandleDestroy = 7
	// zwlr_foreign_toplevel_handle_v1.state
	wlrStateActivated = 2
)

// Wlroots reads focus from the foreign-toplevel protocol and idleness from
// ext-idle-notify-v1, so it needs no helper process or compositor script
// (ADR 12).
//
// The compositor describes every window as a handle and flags the activated
// one. Changes to a handle only count at its done event, so they are held
// until then. The protocol carries no PID, so Window.PID is 0.
type Wlroots struct {
	Now func() int64
	// Socket is the compositor's Wayland socket; "" means
	// $XDG_RUNTIME_DIR/$WAYLAND_DISPLAY.
	Socket string
	// Index maps lowercased WM_CLASS to desktop-entry IDs, as for X11. Most
	// app IDs already are desktop-entry IDs, but XWayland clients report their
	// WM_CLASS (apps.WMClassIndex).
	Index func() map[string]string

	mu            sync.Mutex
	captureTitles bool

	indexOnce sync.Once
	index     map[string]string
}

func (p *Wlroots) ID() string { return "wlroots" }

func (p *Wlroots) socket() string {
	if p.Socket != "" {
		return p.Socket
	}
	return WaylandSocket()
}

func (p *Wlroots) now() int64 {
	if p.Now != nil {
		return p.Now()
	}
	return time.Now().UnixMilli()
}

// Available when the compositor answers on its socket and offers the
// protocol. GNOME and KWin do not offer it, so listing this after them is
// only a precaution.
func (p *Wlroots) Available(context.Context) bool {
	c, err := net.DialTimeout("unix", p.socket(), 5*time.Second)
	if err != nil {
		return false
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(5 * time.Second))
	_, names, err := (&wlConn{c: c, nextID: wlDisplayID}).globals()
	_, ok := names[wlrManagerInterface]
	return err == nil && ok
}

// SetCaptureTitles decides whether Window.Title is filled in.
func (p *Wlroots) SetCaptureTitles(capture bool) {
	p.mu.Lock()
	p.captureTitles = capture
	p.mu.Unlock()
}

// appIDFor prefers the desktop-entry ID: an app ID that is really a WM_CLASS
// is mapped through Index, and anything else is kept as it is.
func (p *Wlroots) appIDFor(appID string) string {
	p.indexOnce.Do(func() {
		if p.Index != nil {
			p.index = p.Index()
		}
	})
	if id, ok := p.index[strings.ToLower(appID)]; ok {
		return id
	}
	return appID
}

// wlrState is what the compositor said about a toplevel.
type wlrState struct {
	appID, title string
	activated    bool
}

// wlrToplevel holds the last complete state, and changes waiting for done.
type wlrToplevel struct{ cur, pend wlrState }

// OnFocusChange reports the activated window, then every change. The
// compositor lists the existing windows when the manager is bound, so the
// window focused now is reported without waiting for a switch. Nothing is
// reported when no window is activated, as for X11.
func (p *Wlroots) OnFocusChange(cb func(Window)) func() {
	c, err := net.DialTimeout("unix", p.socket(), 5*time.Second)
	if err != nil {
		log.Printf("[wlroots] cannot connect to the compositor: %v", err)
		return func() {}
	}
	w := &wlConn{c: c, nextID: wlDisplayID}
	// Setup must not hang the daemon on a compositor that never answers.
	c.SetDeadline(time.Now().Add(5 * time.Second))
	registry, names, err := w.globals()
	global, ok := names[wlrManagerInterface]
	if err == nil && !ok {
		err = errors.New("the compositor does not support wlr-foreign-toplevel-management")
	}
	manager := w.newID()
	if err == nil {
		err = w.send(registry, opRegistryBind, global, wlrManagerInterface, uint32(1), manager)
	}
	if err != nil {
		log.Printf("[wlroots] focus tracking unavailable: %v", err)
		c.Close()
		return func() {}
	}
	c.SetDeadline(time.Time{})

	// Only the reader goroutine touches the state. Unsubscribing must not
	// wait for cb: the daemon unsubscribes while holding the lock cb takes.
	var stopped atomic.Bool
	go func() {
		toplevels := map[uint32]*wlrToplevel{}
		var active uint32 // the activated handle, 0 for none
		var reported struct {
			handle uint32
			appID  string
		}
		// evaluate runs after each change to the activated window.
		evaluate := func() {
			if active == 0 {
				reported.handle = 0 // the same window activated again is news
				return
			}
			t := toplevels[active].cur
			if t.appID == "" || (reported.handle == active && reported.appID == t.appID) {
				return
			}
			reported.handle, reported.appID = active, t.appID
			p.mu.Lock()
			capture := p.captureTitles
			p.mu.Unlock()
			win := Window{AppID: p.appIDFor(t.appID), Ts: p.now()}
			if capture {
				win.Title = t.title
			}
			if !stopped.Load() {
				cb(win)
			}
		}
		for {
			object, opcode, body, err := w.read()
			if err != nil {
				return // closed by unsubscribe, or the compositor went away
			}
			if object == wlDisplayID && opcode == evDisplayError {
				log.Print(displayError(body))
				return
			}
			if object == manager {
				switch opcode {
				case evManagerToplevel:
					args, err := wlArgs(body, "u")
					if err != nil {
						log.Printf("[wlroots] %v", err)
						return
					}
					toplevels[args[0].(uint32)] = &wlrToplevel{}
				case evManagerFinished:
					return
				}
				continue
			}
			t := toplevels[object]
			if t == nil {
				continue
			}
			switch opcode {
			case evHandleTitle, evHandleAppID:
				if args, err := wlArgs(body, "s"); err == nil {
					if opcode == evHandleTitle {
						t.pend.title = args[0].(string)
					} else {
						t.pend.appID = args[0].(string)
					}
				}
			case evHandleState:
				args, err := wlArgs(body, "a")
				if err != nil {
					continue
				}
				t.pend.activated = false
				states := args[0].([]byte)
				for i := 0; i+4 <= len(states); i += 4 {
					if wlOrder.Uint32(states[i:]) == wlrStateActivated {
						t.pend.activated = true
					}
				}
			case evHandleDone:
				t.cur = t.pend
				if t.cur.activated {
					active = object
				} else if active == object {
					active = 0
				}
				evaluate()
			case evHandleClosed:
				delete(toplevels, object)
				w.send(object, opHandleDestroy)
				if active == object {
					active = 0
					evaluate()
				}
			}
		}
	}()
	return func() {
		stopped.Store(true)
		c.Close()
	}
}

// OnIdleChange uses ext-idle-notify-v1, as the KDE provider does.
func (p *Wlroots) OnIdleChange(cb func(bool), thresholdMs int64) func() {
	stop, err := WatchWaylandIdle(p.socket(), thresholdMs, cb)
	if err != nil {
		log.Printf("[wlroots] idle detection unavailable, so time away will be counted: %v", err)
		return func() {}
	}
	return stop
}
