package focus

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"
)

// A minimal Wayland client for ext-idle-notify-v1, the idle protocol KWin,
// sway, Hyprland and other wlroots compositors implement. It speaks only the
// handful of messages it needs, so it pulls in no Wayland library and no cgo.
//
// Wire format: each message is the object ID, then (size<<16 | opcode) where
// size includes this 8-byte header, then 4-byte-aligned arguments in the host
// byte order. A string is its length including the NUL, the bytes, the NUL,
// and padding.

const (
	wlDisplayID = 1

	// wl_display
	opDisplaySync        = 0
	opDisplayGetRegistry = 1
	evDisplayError       = 0
	// wl_registry
	opRegistryBind = 0
	evRegistryGlob = 0
	// wl_callback
	evCallbackDone = 0
	// ext_idle_notifier_v1
	opNotifierGetIdleNotification = 1
	// ext_idle_notification_v1
	evIdled   = 0
	evResumed = 1
)

var wlOrder = binary.NativeEndian

// WaylandSocket is $WAYLAND_DISPLAY, resolved against $XDG_RUNTIME_DIR unless
// it is absolute, as libwayland does.
func WaylandSocket() string {
	name := os.Getenv("WAYLAND_DISPLAY")
	if name == "" {
		name = "wayland-0"
	}
	if filepath.IsAbs(name) {
		return name
	}
	return filepath.Join(os.Getenv("XDG_RUNTIME_DIR"), name)
}

type wlConn struct {
	c      net.Conn
	nextID uint32
}

func (w *wlConn) newID() uint32 {
	w.nextID++
	return w.nextID
}

func (w *wlConn) send(object uint32, opcode uint16, args ...any) error {
	body := []byte{}
	for _, a := range args {
		switch v := a.(type) {
		case uint32:
			body = wlOrder.AppendUint32(body, v)
		case string:
			body = wlOrder.AppendUint32(body, uint32(len(v)+1))
			body = append(body, v...)
			body = append(body, 0)
			for len(body)%4 != 0 {
				body = append(body, 0)
			}
		default:
			panic(fmt.Sprintf("wayland: unsupported argument %T", a))
		}
	}
	msg := wlOrder.AppendUint32(nil, object)
	msg = wlOrder.AppendUint32(msg, uint32(8+len(body))<<16|uint32(opcode))
	_, err := w.c.Write(append(msg, body...))
	return err
}

// read returns the next event.
func (w *wlConn) read() (object uint32, opcode uint16, body []byte, err error) {
	var hdr [8]byte
	if _, err = io.ReadFull(w.c, hdr[:]); err != nil {
		return
	}
	object = wlOrder.Uint32(hdr[0:])
	word := wlOrder.Uint32(hdr[4:])
	size, opcode := word>>16, uint16(word)
	if size < 8 {
		return 0, 0, nil, fmt.Errorf("wayland: bad message size %d", size)
	}
	body = make([]byte, size-8)
	_, err = io.ReadFull(w.c, body)
	return
}

// wlArgs decodes a body with uint32 ('u') and string ('s') arguments.
func wlArgs(body []byte, sig string) ([]any, error) {
	var out []any
	for _, t := range sig {
		if len(body) < 4 {
			return nil, errors.New("wayland: short message")
		}
		n := wlOrder.Uint32(body)
		body = body[4:]
		switch t {
		case 'u':
			out = append(out, n)
		case 's':
			padded := (int(n) + 3) &^ 3
			if n == 0 {
				out = append(out, "")
				continue
			}
			if padded > len(body) {
				return nil, errors.New("wayland: short string")
			}
			out = append(out, string(body[:n-1]))
			body = body[padded:]
		}
	}
	return out, nil
}

func displayError(body []byte) error {
	args, err := wlArgs(body, "uus")
	if err != nil {
		return err
	}
	return fmt.Errorf("wayland: protocol error %d on object %d: %s", args[1], args[0], args[2])
}

// roundtrip sends wl_display.sync and hands every event before its reply to
// handle.
func (w *wlConn) roundtrip(handle func(object uint32, opcode uint16, body []byte) error) error {
	cb := w.newID()
	if err := w.send(wlDisplayID, opDisplaySync, cb); err != nil {
		return err
	}
	for {
		object, opcode, body, err := w.read()
		if err != nil {
			return err
		}
		switch {
		case object == cb && opcode == evCallbackDone:
			return nil
		case object == wlDisplayID && opcode == evDisplayError:
			return displayError(body)
		case handle != nil:
			if err := handle(object, opcode, body); err != nil {
				return err
			}
		}
	}
}

// WatchWaylandIdle asks the compositor to report when there has been no
// input for thresholdMs, and when input resumes. Like the screen blanking it
// is meant for, it honours idle inhibitors, so a playing video is not idle.
// cb is called on each change, from its own goroutine, until stop.
func WatchWaylandIdle(socket string, thresholdMs int64, cb func(idle bool)) (stop func(), err error) {
	c, err := net.DialTimeout("unix", socket, 5*time.Second)
	if err != nil {
		return nil, err
	}
	w := &wlConn{c: c, nextID: wlDisplayID}
	fail := func(err error) (func(), error) {
		c.Close()
		return nil, err
	}
	// Setup must not hang the daemon on a compositor that never answers.
	c.SetDeadline(time.Now().Add(5 * time.Second))

	registry := w.newID()
	if err := w.send(wlDisplayID, opDisplayGetRegistry, registry); err != nil {
		return fail(err)
	}
	var seatName, notifierName uint32
	var haveSeat, haveNotifier bool
	err = w.roundtrip(func(object uint32, opcode uint16, body []byte) error {
		if object != registry || opcode != evRegistryGlob {
			return nil
		}
		args, err := wlArgs(body, "usu")
		if err != nil {
			return err
		}
		switch args[1].(string) {
		case "wl_seat":
			if !haveSeat { // the first seat is the user's
				seatName, haveSeat = args[0].(uint32), true
			}
		case "ext_idle_notifier_v1":
			notifierName, haveNotifier = args[0].(uint32), true
		}
		return nil
	})
	if err != nil {
		return fail(err)
	}
	if !haveNotifier {
		return fail(errors.New("the compositor does not support ext-idle-notify-v1"))
	}
	if !haveSeat {
		return fail(errors.New("the compositor has no seat"))
	}

	seat, notifier, notification := w.newID(), w.newID(), w.newID()
	timeout := uint32(min(max(thresholdMs, 1), int64(^uint32(0))))
	if err := w.send(registry, opRegistryBind, seatName, "wl_seat", uint32(1), seat); err != nil {
		return fail(err)
	}
	if err := w.send(registry, opRegistryBind, notifierName, "ext_idle_notifier_v1", uint32(1), notifier); err != nil {
		return fail(err)
	}
	if err := w.send(notifier, opNotifierGetIdleNotification, notification, timeout, seat); err != nil {
		return fail(err)
	}
	// Surfaces a protocol error from the requests above now, not later.
	var early []uint16
	err = w.roundtrip(func(object uint32, opcode uint16, _ []byte) error {
		if object == notification {
			early = append(early, opcode)
		}
		return nil
	})
	if err != nil {
		return fail(err)
	}
	c.SetDeadline(time.Time{})

	// Only the reader goroutine touches idle. stop must not wait for cb: the
	// daemon unsubscribes while holding the lock cb takes.
	var stopped atomic.Bool
	go func() {
		idle := false
		report := func(opcode uint16) {
			now := opcode == evIdled
			if stopped.Load() || (opcode != evIdled && opcode != evResumed) || now == idle {
				return
			}
			idle = now
			cb(idle)
		}
		for _, op := range early {
			report(op)
		}
		for {
			object, opcode, body, err := w.read()
			if err != nil {
				return // closed by stop, or the compositor went away
			}
			switch {
			case object == notification:
				report(opcode)
			case object == wlDisplayID && opcode == evDisplayError:
				log.Print(displayError(body))
				return
			}
		}
	}()
	return func() {
		stopped.Store(true)
		c.Close()
	}, nil
}
