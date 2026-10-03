package focus

import (
	"bufio"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

// privateBus starts a throwaway session bus, so tests never touch the real
// desktop. Skipped when dbus-daemon isn't installed.
func privateBus(t *testing.T) func() (*dbus.Conn, error) {
	t.Helper()
	if _, err := exec.LookPath("dbus-daemon"); err != nil {
		t.Skip("dbus-daemon not installed")
	}
	cmd := exec.Command("dbus-daemon", "--session", "--nofork", "--print-address=1")
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	addr, err := bufio.NewReader(out).ReadString('\n')
	if err != nil {
		t.Fatal(err)
	}
	addr = strings.TrimSpace(addr)
	return func() (*dbus.Conn, error) { return dbus.Connect(addr) }
}

func connect(t *testing.T, bus func() (*dbus.Conn, error)) *dbus.Conn {
	t.Helper()
	conn, err := bus()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

// fakeExtension behaves like adapters/gnome-extension in subscribed mode.
type fakeExtension struct {
	conn *dbus.Conn

	mu         sync.Mutex
	subscriber string
	capture    bool
	focus      [3]any // appId, title, pid
}

func (e *fakeExtension) Subscribe(sender dbus.Sender) *dbus.Error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.subscriber != "" && e.subscriber != string(sender) {
		return dbus.NewError("org.freedesktop.DBus.Error.AccessDenied", nil)
	}
	e.subscriber = string(sender)
	return nil
}

func (e *fakeExtension) SetCaptureTitles(sender dbus.Sender, capture bool) *dbus.Error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if string(sender) != e.subscriber {
		return dbus.NewError("org.freedesktop.DBus.Error.AccessDenied", nil)
	}
	e.capture = capture
	return nil
}

func (e *fakeExtension) GetFocus(sender dbus.Sender) (string, string, uint32, *dbus.Error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if string(sender) != e.subscriber || e.focus[0] == nil {
		return "", "", 0, nil
	}
	return e.focus[0].(string), e.titleLocked(), e.focus[2].(uint32), nil
}

func (e *fakeExtension) titleLocked() string {
	if !e.capture {
		return ""
	}
	return e.focus[1].(string)
}

// switchTo changes focus and signals the subscriber only.
func (e *fakeExtension) switchTo(t *testing.T, appID, title string, pid uint32) {
	t.Helper()
	e.mu.Lock()
	e.focus = [3]any{appID, title, pid}
	to, body := e.subscriber, []any{appID, e.titleLocked(), pid}
	e.mu.Unlock()
	if to == "" {
		return
	}
	if err := sendSignal(e.conn, to, focusPath, focusInterface, "FocusChanged", body...); err != nil {
		t.Fatal(err)
	}
}

// sendSignal sends a signal addressed to one connection (dbus.Conn.Emit only broadcasts).
func sendSignal(conn *dbus.Conn, to string, path dbus.ObjectPath, iface, member string, body ...any) error {
	msg := &dbus.Message{
		Type: dbus.TypeSignal,
		Headers: map[dbus.HeaderField]dbus.Variant{
			dbus.FieldPath:      dbus.MakeVariant(path),
			dbus.FieldInterface: dbus.MakeVariant(iface),
			dbus.FieldMember:    dbus.MakeVariant(member),
			dbus.FieldSignature: dbus.MakeVariant(dbus.SignatureOf(body...)),
		},
		Body: body,
	}
	if to != "" {
		msg.Headers[dbus.FieldDestination] = dbus.MakeVariant(to)
	}
	return conn.Send(msg, nil).Err
}

// startExtension exports the fake and claims the extension's name, with the
// given window focused (appID "" for none) before any client can ask.
func startExtension(t *testing.T, bus func() (*dbus.Conn, error), appID, title string, pid uint32) *fakeExtension {
	t.Helper()
	conn := connect(t, bus)
	e := &fakeExtension{conn: conn}
	if appID != "" {
		e.focus = [3]any{appID, title, pid}
	}
	if err := conn.Export(e, focusPath, focusInterface); err != nil {
		t.Fatal(err)
	}
	if reply, err := conn.RequestName(extensionName, dbus.NameFlagDoNotQueue); err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
		t.Fatalf("owning name: %v %v", reply, err)
	}
	return e
}

func (e *fakeExtension) state() (string, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.subscriber, e.capture
}

func waitFor(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func nextWindow(t *testing.T, got <-chan Window) Window {
	t.Helper()
	select {
	case w := <-got:
		return w
	case <-time.After(3 * time.Second):
		t.Fatal("no focus change")
		return Window{}
	}
}

func noWindow(t *testing.T, got <-chan Window) {
	t.Helper()
	select {
	case w := <-got:
		t.Fatalf("unexpected focus change %+v", w)
	case <-time.After(200 * time.Millisecond):
	}
}

func TestGnomeSubscribesAndReportsFocus(t *testing.T) {
	bus := privateBus(t)
	ext := startExtension(t, bus, "org.gnome.Ptyxis", "~/src", 7)

	g := &Gnome{Connect: bus, Now: func() int64 { return 42 }}
	got := make(chan Window, 8)
	stop := g.OnFocusChange(func(w Window) { got <- w })
	defer stop()

	// The current window is fetched on attach, with no title: capture is off.
	if w := nextWindow(t, got); w != (Window{AppID: "org.gnome.Ptyxis", PID: 7, Ts: 42}) {
		t.Errorf("initial window = %+v", w)
	}
	conn, _ := g.bus()
	if sub, capture := ext.state(); sub != conn.Names()[0] || capture {
		t.Errorf("subscriber %q capture %v; want this connection, false", sub, capture)
	}

	ext.switchTo(t, "org.mozilla.firefox", "Private chat", 9)
	if w := nextWindow(t, got); w.AppID != "org.mozilla.firefox" || w.Title != "" {
		t.Errorf("window = %+v", w)
	}

	// Opting in reaches the extension; titles follow.
	g.SetCaptureTitles(true)
	waitFor(t, "title capture", func() bool { _, c := ext.state(); return c })
	ext.switchTo(t, "code", "main.go", 11)
	if w := nextWindow(t, got); w.Title != "main.go" {
		t.Errorf("window = %+v, want title", w)
	}
}

// S6: a FocusChanged from anyone but the extension's owner is ignored,
// broadcast or addressed to us.
func TestGnomeIgnoresImpostors(t *testing.T) {
	bus := privateBus(t)
	startExtension(t, bus, "", "", 0)
	g := &Gnome{Connect: bus}
	got := make(chan Window, 8)
	defer g.OnFocusChange(func(w Window) { got <- w })()

	conn, _ := g.bus()
	waitFor(t, "subscription", func() bool { g.mu.Lock(); defer g.mu.Unlock(); return g.owner != "" })
	impostor := connect(t, bus)
	sendSignal(impostor, "", focusPath, focusInterface, "FocusChanged", "evil", "fake", uint32(1))
	sendSignal(impostor, conn.Names()[0], focusPath, focusInterface, "FocusChanged", "evil", "fake", uint32(1))
	noWindow(t, got)
}

func TestGnomeReattachesWhenTheExtensionReturns(t *testing.T) {
	bus := privateBus(t)
	g := &Gnome{Connect: bus}
	got := make(chan Window, 8)
	defer g.OnFocusChange(func(w Window) { got <- w })()
	noWindow(t, got) // no extension yet: waiting, not failing

	ext := startExtension(t, bus, "code", "", 1)
	if w := nextWindow(t, got); w.AppID != "code" {
		t.Errorf("window = %+v", w)
	}

	// The lock screen turns the extension off; a new instance appears after.
	ext.conn.Close()
	again := startExtension(t, bus, "org.gnome.Nautilus", "", 2)
	if w := nextWindow(t, got); w.AppID != "org.gnome.Nautilus" {
		t.Errorf("window = %+v", w)
	}
	if sub, _ := again.state(); sub == "" {
		t.Error("did not subscribe to the new extension instance")
	}
}

// fakeIdleMonitor implements the parts of Mutter's IdleMonitor we use.
type fakeIdleMonitor struct {
	conn *dbus.Conn

	mu       sync.Mutex
	next     uint32
	idleMs   uint64
	idleW    map[uint32]uint64 // id -> threshold
	activeW  map[uint32]bool
	watchers map[uint32]string // id -> owning connection
}

func (m *fakeIdleMonitor) AddIdleWatch(sender dbus.Sender, ms uint64) (uint32, *dbus.Error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.next++
	m.idleW[m.next], m.watchers[m.next] = ms, string(sender)
	return m.next, nil
}

func (m *fakeIdleMonitor) AddUserActiveWatch(sender dbus.Sender) (uint32, *dbus.Error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.next++
	m.activeW[m.next], m.watchers[m.next] = true, string(sender)
	return m.next, nil
}

func (m *fakeIdleMonitor) RemoveWatch(id uint32) *dbus.Error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.idleW, id)
	delete(m.activeW, id)
	return nil
}

func (m *fakeIdleMonitor) GetIdletime() (uint64, *dbus.Error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.idleMs, nil
}

// goIdle fires every idle watch; resume fires (and removes) active watches.
func (m *fakeIdleMonitor) fire(t *testing.T, idle bool) {
	t.Helper()
	m.mu.Lock()
	var fire []uint32
	if idle {
		for id := range m.idleW {
			fire = append(fire, id)
		}
	} else {
		for id := range m.activeW {
			fire = append(fire, id)
			delete(m.activeW, id)
		}
	}
	m.mu.Unlock()
	for _, id := range fire {
		if err := sendSignal(m.conn, m.watchers[id], idlePath, idleInterface, "WatchFired", id); err != nil {
			t.Fatal(err)
		}
	}
}

func TestGnomeIdleUsesMutterWatches(t *testing.T) {
	bus := privateBus(t)
	conn := connect(t, bus)
	m := &fakeIdleMonitor{conn: conn, idleW: map[uint32]uint64{}, activeW: map[uint32]bool{}, watchers: map[uint32]string{}}
	conn.Export(m, idlePath, idleInterface)
	conn.RequestName(idleService, dbus.NameFlagDoNotQueue)

	g := &Gnome{Connect: bus}
	got := make(chan bool, 8)
	stop := g.OnIdleChange(func(idle bool) { got <- idle }, 180_000)
	next := func() bool {
		select {
		case v := <-got:
			return v
		case <-time.After(3 * time.Second):
			t.Fatal("no idle change")
			return false
		}
	}

	waitFor(t, "idle watch", func() bool {
		m.mu.Lock()
		defer m.mu.Unlock()
		for _, ms := range m.idleW {
			return ms == 180_000
		}
		return false
	})
	m.fire(t, true)
	if !next() {
		t.Error("want idle")
	}
	waitFor(t, "active watch", func() bool { m.mu.Lock(); defer m.mu.Unlock(); return len(m.activeW) == 1 })
	m.fire(t, false)
	if next() {
		t.Error("want active")
	}

	stop()
	waitFor(t, "watch removed", func() bool { m.mu.Lock(); defer m.mu.Unlock(); return len(m.idleW) == 0 })
}

func TestGnomeReportsIdleAtSubscribeWhenAlreadyIdle(t *testing.T) {
	bus := privateBus(t)
	conn := connect(t, bus)
	m := &fakeIdleMonitor{conn: conn, idleMs: 600_000, idleW: map[uint32]uint64{}, activeW: map[uint32]bool{}, watchers: map[uint32]string{}}
	conn.Export(m, idlePath, idleInterface)
	conn.RequestName(idleService, dbus.NameFlagDoNotQueue)

	g := &Gnome{Connect: bus}
	got := make(chan bool, 1)
	defer g.OnIdleChange(func(idle bool) { got <- idle }, 180_000)()
	select {
	case idle := <-got:
		if !idle {
			t.Error("want idle")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("already-idle state not reported")
	}
}
