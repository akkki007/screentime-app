package focus

import (
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// fakeCompositor is just enough of a Wayland compositor for WatchWaylandIdle:
// it advertises globals, answers sync, and records the idle notification.
type fakeCompositor struct {
	t       *testing.T
	socket  string
	globals []string
	// requested gets (notification id, timeout) once the client asks.
	requested chan [2]uint32
	conns     chan *wlConn
}

func newFakeCompositor(t *testing.T, globals ...string) *fakeCompositor {
	t.Helper()
	f := &fakeCompositor{
		t:         t,
		socket:    filepath.Join(t.TempDir(), "wayland-test"),
		globals:   globals,
		requested: make(chan [2]uint32, 1),
		conns:     make(chan *wlConn, 1),
	}
	l, err := net.Listen("unix", f.socket)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go f.serve(&wlConn{c: c})
		}
	}()
	return f
}

func (f *fakeCompositor) serve(w *wlConn) {
	defer w.c.Close()
	f.conns <- w
	registry := uint32(0)
	notifier := uint32(0)
	for {
		object, opcode, body, err := w.read()
		if err != nil {
			return
		}
		switch {
		case object == wlDisplayID && opcode == opDisplayGetRegistry:
			args, _ := wlArgs(body, "u")
			registry = args[0].(uint32)
			for i, g := range f.globals {
				w.send(registry, evRegistryGlob, uint32(i+1), g, uint32(1))
			}
		case object == wlDisplayID && opcode == opDisplaySync:
			args, _ := wlArgs(body, "u")
			w.send(args[0].(uint32), evCallbackDone, uint32(0))
		case object == registry && opcode == opRegistryBind:
			args, _ := wlArgs(body, "usuu")
			if args[1] == "ext_idle_notifier_v1" {
				notifier = args[3].(uint32)
			}
		case object == notifier && opcode == opNotifierGetIdleNotification:
			args, _ := wlArgs(body, "uuu")
			f.requested <- [2]uint32{args[0].(uint32), args[1].(uint32)}
		}
	}
}

func TestWaylandIdleReportsCrossings(t *testing.T) {
	f := newFakeCompositor(t, "wl_compositor", "wl_seat", "ext_idle_notifier_v1")
	got := make(chan bool, 4)
	stop, err := WatchWaylandIdle(f.socket, 120_000, func(idle bool) { got <- idle })
	if err != nil {
		t.Fatal(err)
	}
	defer stop()
	w := <-f.conns
	req := <-f.requested
	if req[1] != 120_000 {
		t.Fatalf("timeout = %d, want 120000", req[1])
	}
	notification := req[0]

	// resumed before idled, and a repeated idled, are not crossings.
	for _, op := range []uint16{evResumed, evIdled, evIdled, evResumed} {
		w.send(notification, op)
	}
	for _, want := range []bool{true, false} {
		select {
		case idle := <-got:
			if idle != want {
				t.Fatalf("idle = %v, want %v", idle, want)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("no idle change")
		}
	}
	select {
	case idle := <-got:
		t.Fatalf("extra idle change %v", idle)
	case <-time.After(100 * time.Millisecond):
	}

	stop()
	w.send(notification, evIdled)
	select {
	case idle := <-got:
		t.Fatalf("reported %v after stop", idle)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestWaylandIdleNeedsTheProtocol(t *testing.T) {
	f := newFakeCompositor(t, "wl_seat")
	if _, err := WatchWaylandIdle(f.socket, 1000, func(bool) {}); err == nil || !strings.Contains(err.Error(), "ext-idle-notify-v1") {
		t.Fatalf("err = %v", err)
	}
}

func TestWaylandIdleNoCompositor(t *testing.T) {
	if _, err := WatchWaylandIdle(filepath.Join(t.TempDir(), "none"), 1000, func(bool) {}); err == nil {
		t.Fatal("no error without a compositor")
	}
}

func TestWaylandStringsRoundTrip(t *testing.T) {
	for _, s := range []string{"", "a", "abc", "abcd", "ext_idle_notifier_v1"} {
		client, server := net.Pipe()
		go (&wlConn{c: client}).send(7, 3, uint32(42), s, uint32(9))
		_, opcode, body, err := (&wlConn{c: server}).read()
		if err != nil || opcode != 3 {
			t.Fatalf("%q: opcode %d, %v", s, opcode, err)
		}
		if len(body)%4 != 0 {
			t.Fatalf("%q: body not padded: %d bytes", s, len(body))
		}
		args, err := wlArgs(body, "usu")
		if err != nil || args[0] != uint32(42) || args[1] != s || args[2] != uint32(9) {
			t.Fatalf("%q: got %v, %v", s, args, err)
		}
	}
}

func TestWaylandSocket(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "/run/user/1000")
	t.Setenv("WAYLAND_DISPLAY", "")
	if got := WaylandSocket(); got != "/run/user/1000/wayland-0" {
		t.Errorf("default: %s", got)
	}
	t.Setenv("WAYLAND_DISPLAY", "wayland-1")
	if got := WaylandSocket(); got != "/run/user/1000/wayland-1" {
		t.Errorf("relative: %s", got)
	}
	t.Setenv("WAYLAND_DISPLAY", "/tmp/w")
	if got := WaylandSocket(); got != "/tmp/w" {
		t.Errorf("absolute: %s", got)
	}
}
