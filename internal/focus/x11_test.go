package focus

import (
	"os"
	"testing"
	"time"

	"github.com/jezek/xgb"
	"github.com/jezek/xgb/xproto"
)

func TestParseWMClass(t *testing.T) {
	cases := map[string]string{
		"Navigator\x00firefox\x00": "firefox",
		"code\x00Code\x00":         "Code",
		"onlyinstance\x00\x00":     "onlyinstance",
		"":                         "",
	}
	for in, want := range cases {
		if got := ParseWMClass([]byte(in)); got != want {
			t.Errorf("ParseWMClass(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAppIDPrefersTheDesktopEntry(t *testing.T) {
	x := &X11{Index: func() map[string]string { return map[string]string{"code": "code-oss"} }}
	if got := x.appIDFor("Code"); got != "code-oss" {
		t.Errorf("appIDFor(Code) = %q", got)
	}
	if got := x.appIDFor("Gimp"); got != "Gimp" {
		t.Errorf("appIDFor(Gimp) = %q", got)
	}
}

// The rest needs a private X server with no window manager (Xvfb in CI,
// rootless Xwayland locally), named by SCREENTIME_TEST_X_DISPLAY. The test
// plays the window manager: it sets _NET_ACTIVE_WINDOW on the root itself.
func testDisplay(t *testing.T) string {
	t.Helper()
	d := os.Getenv("SCREENTIME_TEST_X_DISPLAY")
	if d == "" {
		t.Skip("set SCREENTIME_TEST_X_DISPLAY to a private X server (never your desktop's)")
	}
	return d
}

type wm struct {
	t     *testing.T
	conn  *xgb.Conn
	root  xproto.Window
	atoms x11Atoms
}

func newWM(t *testing.T, display string) *wm {
	t.Helper()
	conn, err := xgb.NewConnDisplay(display)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(conn.Close)
	atoms, err := internAtoms(conn)
	if err != nil {
		t.Fatal(err)
	}
	return &wm{t, conn, xproto.Setup(conn).DefaultScreen(conn).Root, atoms}
}

func (m *wm) set(win xproto.Window, prop, typ xproto.Atom, format byte, data []byte) {
	m.t.Helper()
	unit := uint32(len(data))
	if format == 32 {
		unit /= 4
	}
	if err := xproto.ChangePropertyChecked(m.conn, xproto.PropModeReplace, win, prop, typ, format, unit, data).Check(); err != nil {
		m.t.Fatal(err)
	}
}

// window creates an unmapped window (nothing is shown) with the given props.
func (m *wm) window(class, title string, pid uint32) xproto.Window {
	m.t.Helper()
	id, err := xproto.NewWindowId(m.conn)
	if err != nil {
		m.t.Fatal(err)
	}
	if err := xproto.CreateWindowChecked(m.conn, 0, id, m.root, 0, 0, 1, 1, 0,
		xproto.WindowClassInputOnly, 0, 0, nil).Check(); err != nil {
		m.t.Fatal(err)
	}
	if class != "" {
		m.set(id, xproto.AtomWmClass, xproto.AtomString, 8, []byte(class))
	}
	m.set(id, m.atoms.wmName, m.atoms.utf8, 8, []byte(title))
	buf := make([]byte, 4)
	xgb.Put32(buf, pid)
	m.set(id, m.atoms.wmPID, xproto.AtomCardinal, 32, buf)
	return id
}

func (m *wm) activate(win xproto.Window) {
	m.t.Helper()
	buf := make([]byte, 4)
	xgb.Put32(buf, uint32(win))
	m.set(m.root, m.atoms.activeWindow, xproto.AtomWindow, 32, buf)
}

func TestX11ReportsTheActiveWindowThenEachChange(t *testing.T) {
	display := testDisplay(t)
	m := newWM(t, display)
	firefox := m.window("Navigator\x00firefox\x00", "Home", 7)
	code := m.window("code\x00Code\x00", "main.go", 9)
	untitled := m.window("", "no class", 1)
	m.activate(firefox)

	x := &X11{Display: display, Now: func() int64 { return 1234 },
		Index: func() map[string]string { return map[string]string{"firefox": "org.mozilla.firefox"} }}
	got := make(chan Window, 8)
	stop := x.OnFocusChange(func(w Window) { got <- w })

	if w := nextWindow(t, got); w != (Window{AppID: "org.mozilla.firefox", Title: "Home", PID: 7, Ts: 1234}) {
		t.Errorf("initial = %+v", w)
	}
	m.activate(untitled) // no WM_CLASS: skipped
	m.activate(0)        // no window: skipped
	m.activate(code)
	if w := nextWindow(t, got); w.AppID != "Code" || w.Title != "main.go" || w.PID != 9 {
		t.Errorf("next = %+v", w)
	}
	noWindow(t, got)

	stop()
	m.activate(firefox)
	noWindow(t, got)
}

func TestX11IdleReportsCrossings(t *testing.T) {
	display := testDisplay(t)
	x := &X11{Display: display, IdlePoll: 20 * time.Millisecond}
	got := make(chan bool, 4)
	// Nobody types on a private server, so any idle time crosses 1 ms.
	stop := x.OnIdleChange(func(idle bool) { got <- idle }, 1)
	defer stop()
	select {
	case idle := <-got:
		if !idle {
			t.Error("want idle")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no idle report")
	}
	select {
	case v := <-got:
		t.Errorf("repeated report %v: only crossings should be reported", v)
	case <-time.After(200 * time.Millisecond):
	}
}
