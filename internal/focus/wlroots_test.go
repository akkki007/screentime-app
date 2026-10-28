//go:build linux

package focus

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

const (
	tlA = uint32(0xff000001)
	tlB = uint32(0xff000002)
)

// announce introduces toplevel id with its app ID and title, as the
// compositor does for a window: properties, then done.
func announce(w *wlConn, manager, id uint32, appID, title string, activated bool) {
	w.send(manager, evManagerToplevel, id)
	w.send(id, evHandleTitle, title)
	w.send(id, evHandleAppID, appID)
	setState(w, id, activated)
}

// setState sends the state array, then done.
func setState(w *wlConn, id uint32, activated bool) {
	states := []byte{}
	if activated {
		states = wlOrder.AppendUint32(states, wlrStateActivated)
	}
	w.send(id, evHandleState, states)
	w.send(id, evHandleDone)
}

// subscribe starts a provider on f and returns its focus changes, the
// compositor's side of the connection and the manager's ID.
func subscribe(t *testing.T, f *fakeCompositor, p *Wlroots) (chan Window, *wlConn, uint32) {
	t.Helper()
	p.Socket = f.socket
	got := make(chan Window, 8)
	stop := p.OnFocusChange(func(w Window) { got <- w })
	t.Cleanup(stop)
	select {
	case manager := <-f.managers:
		return got, <-f.conns, manager
	case <-time.After(2 * time.Second):
		t.Fatal("the provider did not bind the manager")
		return nil, nil, 0
	}
}

func none(t *testing.T, got chan Window) {
	t.Helper()
	select {
	case w := <-got:
		t.Fatalf("unexpected focus change %+v", w)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestWlrootsFollowsTheActivatedToplevel(t *testing.T) {
	f := newFakeCompositor(t, "wl_seat", wlrManagerInterface)
	got, w, manager := subscribe(t, f, &Wlroots{Now: func() int64 { return 42 }})

	// The window focused at bind time is reported at once; the other is not.
	announce(w, manager, tlA, "org.mozilla.firefox", "Home", true)
	announce(w, manager, tlB, "org.kde.konsole", "~", false)
	if win := next(t, got); win != (Window{AppID: "org.mozilla.firefox", Ts: 42}) {
		t.Fatalf("first = %+v", win)
	}
	none(t, got)

	// Deactivation before activation, then the reverse order.
	setState(w, tlA, false)
	setState(w, tlB, true)
	if win := next(t, got); win.AppID != "org.kde.konsole" {
		t.Fatalf("second = %+v", win)
	}
	setState(w, tlB, false)
	setState(w, tlA, true)
	if win := next(t, got); win.AppID != "org.mozilla.firefox" {
		t.Fatalf("third = %+v", win)
	}
	setState(w, tlB, true)
	setState(w, tlA, false)
	if win := next(t, got); win.AppID != "org.kde.konsole" {
		t.Fatalf("fourth = %+v", win)
	}
	none(t, got)

	// Repeated done events for the same state are not changes, and a window
	// with nothing activated reports nothing.
	setState(w, tlB, true)
	setState(w, tlB, false)
	none(t, got)
	setState(w, tlB, true)
	if win := next(t, got); win.AppID != "org.kde.konsole" {
		t.Fatalf("reactivated = %+v", win)
	}
}

func TestWlrootsAppliesChangesAtDone(t *testing.T) {
	f := newFakeCompositor(t, "wl_seat", wlrManagerInterface)
	got, w, manager := subscribe(t, f, &Wlroots{})

	w.send(manager, evManagerToplevel, tlA)
	w.send(tlA, evHandleAppID, "a")
	w.send(tlA, evHandleState, wlOrder.AppendUint32(nil, wlrStateActivated))
	none(t, got) // no done yet
	w.send(tlA, evHandleDone)
	next(t, got)
}

func TestWlrootsTitlesOnlyWithCapture(t *testing.T) {
	f := newFakeCompositor(t, "wl_seat", wlrManagerInterface)
	p := &Wlroots{}
	got, w, manager := subscribe(t, f, p)

	announce(w, manager, tlA, "a", "secret page", true)
	announce(w, manager, tlB, "b", "other secret", false)
	if win := next(t, got); win.Title != "" {
		t.Fatalf("title %q leaked with capture off", win.Title)
	}
	p.SetCaptureTitles(true)
	setState(w, tlA, false)
	setState(w, tlB, true)
	if win := next(t, got); win.AppID != "b" || win.Title != "other secret" {
		t.Fatalf("with capture on: %+v", win)
	}
	p.SetCaptureTitles(false)
	setState(w, tlB, false)
	setState(w, tlA, true)
	if win := next(t, got); win.Title != "" {
		t.Fatalf("title %q after capture was turned off", win.Title)
	}
}

func TestWlrootsClosedFocusedToplevel(t *testing.T) {
	f := newFakeCompositor(t, "wl_seat", wlrManagerInterface)
	got, w, manager := subscribe(t, f, &Wlroots{})

	announce(w, manager, tlA, "a", "", true)
	next(t, got)
	w.send(tlA, evHandleClosed)
	none(t, got) // no window is focused, which is not reported

	// The compositor then activates another window; a closed handle's late
	// events are ignored.
	w.send(tlA, evHandleDone)
	announce(w, manager, tlB, "b", "", true)
	if win := next(t, got); win.AppID != "b" {
		t.Fatalf("after close = %+v", win)
	}
	w.send(tlB, evHandleClosed)
	announce(w, manager, tlA+2, "a", "", true) // the same app, a new window
	if win := next(t, got); win.AppID != "a" {
		t.Fatalf("new window = %+v", win)
	}
}

func TestWlrootsMapsWMClassToDesktopID(t *testing.T) {
	f := newFakeCompositor(t, "wl_seat", wlrManagerInterface)
	got, w, manager := subscribe(t, f, &Wlroots{Index: func() map[string]string {
		return map[string]string{"firefox": "org.mozilla.firefox"}
	}})
	announce(w, manager, tlA, "Firefox", "", true)
	if win := next(t, got); win.AppID != "org.mozilla.firefox" {
		t.Fatalf("app = %q", win.AppID)
	}
	setState(w, tlA, false)
	announce(w, manager, tlB, "org.kde.konsole", "", true)
	if win := next(t, got); win.AppID != "org.kde.konsole" {
		t.Fatalf("app = %q", win.AppID)
	}
}

func TestWlrootsStopSilencesCallbacks(t *testing.T) {
	f := newFakeCompositor(t, "wl_seat", wlrManagerInterface)
	p := &Wlroots{Socket: f.socket}
	got := make(chan Window, 4)
	stop := p.OnFocusChange(func(w Window) { got <- w })
	manager := <-f.managers
	w := <-f.conns
	stop()
	announce(w, manager, tlA, "a", "", true)
	none(t, got)
}

func TestWlrootsAvailable(t *testing.T) {
	has := newFakeCompositor(t, "wl_seat", wlrManagerInterface)
	if !(&Wlroots{Socket: has.socket}).Available(context.Background()) {
		t.Error("not available with the protocol")
	}
	// GNOME and KWin run a compositor too, without this protocol.
	lacks := newFakeCompositor(t, "wl_seat", "ext_idle_notifier_v1")
	if (&Wlroots{Socket: lacks.socket}).Available(context.Background()) {
		t.Error("available without the protocol")
	}
}

func TestWlrootsNoCompositor(t *testing.T) {
	p := &Wlroots{Socket: filepath.Join(t.TempDir(), "none")}
	if p.Available(context.Background()) {
		t.Error("available without a compositor")
	}
	// Both subscriptions fail soft: they log and return a no-op.
	p.OnFocusChange(func(Window) { t.Error("focus change without a compositor") })()
	p.OnIdleChange(func(bool) { t.Error("idle change without a compositor") }, 1000)()
}

func TestWlrootsFocusNeedsTheProtocol(t *testing.T) {
	f := newFakeCompositor(t, "wl_seat")
	p := &Wlroots{Socket: f.socket}
	p.OnFocusChange(func(Window) { t.Error("focus change without the protocol") })()
	select {
	case m := <-f.managers:
		t.Fatalf("bound a manager (%d) that is not advertised", m)
	default:
	}
}

func TestWlrootsIdleUsesExtIdleNotify(t *testing.T) {
	f := newFakeCompositor(t, "wl_seat", "ext_idle_notifier_v1", wlrManagerInterface)
	got := make(chan bool, 2)
	stop := (&Wlroots{Socket: f.socket}).OnIdleChange(func(idle bool) { got <- idle }, 30_000)
	defer stop()
	w := <-f.conns
	req := <-f.requested
	if req[1] != 30_000 {
		t.Fatalf("timeout = %d", req[1])
	}
	w.send(req[0], evIdled)
	select {
	case idle := <-got:
		if !idle {
			t.Fatal("idle = false")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no idle change")
	}
}
