package focus

import (
	"errors"
	"os"
	"reflect"
	"sync"
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

// pollIdle reports threshold crossings only, and survives query errors.
func TestPollIdleReportsCrossingsOnly(t *testing.T) {
	readings := []int64{0, 100, 250, 400, -1, 900, 50, 20}
	var mu sync.Mutex
	next := 0
	query := func() (int64, error) {
		mu.Lock()
		defer mu.Unlock()
		if next >= len(readings) {
			return 0, nil
		}
		v := readings[next]
		next++
		if v < 0 {
			return 0, errors.New("server went away for a moment")
		}
		return v, nil
	}
	got := make(chan bool, 8)
	done := make(chan struct{})
	go pollIdle(done, time.Millisecond, 300, query, func(idle bool) { got <- idle })
	waitFor(t, "all readings", func() bool { mu.Lock(); defer mu.Unlock(); return next >= len(readings) })
	close(done)
	var seen []bool
	for len(got) > 0 {
		seen = append(seen, <-got)
	}
	if want := []bool{true, false}; !reflect.DeepEqual(seen, want) {
		t.Errorf("reports = %v, want %v", seen, want)
	}
}

// On a real server, the idle query works. (How servers count idle time
// without any input devices differs, e.g. Xvfb, so the value isn't checked.)
func TestX11IdleQueryWorks(t *testing.T) {
	display := testDisplay(t)
	conn, err := xgb.NewConnDisplay(display)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := initScreenSaver(conn); err != nil {
		t.Fatalf("MIT-SCREEN-SAVER: %v", err)
	}
	ms, err := idleMs(conn)
	if err != nil {
		t.Fatalf("idle query: %v", err)
	}
	t.Logf("server reports %d ms since the last input", ms)
}
