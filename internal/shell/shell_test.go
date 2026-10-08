package shell

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/akkki007/screentime-app/internal/ipc"
	"github.com/akkki007/screentime-app/internal/rpc"
)

// fakeDaemon answers echo and fail, and implements the version handshake in
// the real server (rpc.Listen), so the tests exercise the real wire format
// and the S3 dial checks.
type fakeDaemon struct{ server *rpc.Server }

func (fakeDaemon) Handle(method string, params json.RawMessage) (any, error) {
	switch method {
	case "echo":
		return json.RawMessage(params), nil
	case "fail":
		return nil, errors.New("nope")
	case "hang":
		time.Sleep(time.Hour)
	}
	return nil, rpc.ErrMethodNotFound
}

func startDaemon(t *testing.T) (path string, d *fakeDaemon) {
	t.Helper()
	path = filepath.Join(t.TempDir(), "run", "daemon.sock")
	d = &fakeDaemon{}
	srv, err := rpc.Listen(path, d)
	if err != nil {
		t.Fatal(err)
	}
	d.server = srv
	t.Cleanup(srv.Close)
	return path, d
}

type events struct {
	mu     sync.Mutex
	states []State
	notes  []string
}

func (e *events) onState(s State) { e.mu.Lock(); e.states = append(e.states, s); e.mu.Unlock() }
func (e *events) onNote(m string, _ json.RawMessage) {
	e.mu.Lock()
	e.notes = append(e.notes, m)
	e.mu.Unlock()
}
func (e *events) snapshot() ([]State, []string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]State(nil), e.states...), append([]string(nil), e.notes...)
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func newConn(path string, ev *events) *Conn {
	return NewConn(Options{
		SocketPath:     func() string { return path },
		Retry:          20 * time.Millisecond,
		OnNotification: ev.onNote,
		OnState:        ev.onState,
	})
}

func TestConnCallsAndRelaysNotifications(t *testing.T) {
	path, d := startDaemon(t)
	ev := &events{}
	c := newConn(path, ev)
	c.Start()
	defer c.Stop()
	waitFor(t, "connection", func() bool { return c.State().Connected })

	got, err := c.Call("echo", map[string]int{"a": 1})
	if err != nil || string(got) != `{"a":1}` {
		t.Fatalf("echo = %s, %v", got, err)
	}
	if _, err := c.Call("fail", nil); err == nil || err.Error() != "nope" {
		t.Fatalf("fail = %v, want the daemon's message", err)
	}

	d.server.Broadcast("event.status", map[string]bool{"paused": true})
	waitFor(t, "notification", func() bool { _, n := ev.snapshot(); return len(n) == 1 })
	if _, n := ev.snapshot(); n[0] != "event.status" {
		t.Fatalf("notification = %v", n)
	}
}

func TestConnReconnectsAfterDaemonRestart(t *testing.T) {
	path, d := startDaemon(t)
	ev := &events{}
	c := newConn(path, ev)
	c.Start()
	defer c.Stop()
	waitFor(t, "first connection", func() bool { return c.State().Connected })

	d.server.Close()
	waitFor(t, "disconnect", func() bool { return !c.State().Connected })
	if _, err := c.Call("echo", nil); !errors.Is(err, ErrDaemonDown) {
		t.Fatalf("call while down = %v, want ErrDaemonDown", err)
	}

	srv, err := rpc.Listen(path, fakeDaemon{})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	waitFor(t, "reconnection", func() bool { return c.State().Connected })
	if _, err := c.Call("echo", 1); err != nil {
		t.Fatal(err)
	}
}

func TestConnStartsBeforeDaemonAndReportsEachErrorOnce(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "run", "daemon.sock")
	ev := &events{}
	c := newConn(path, ev)
	c.Start()
	defer c.Stop()
	time.Sleep(150 * time.Millisecond) // several retries

	states, _ := ev.snapshot()
	if len(states) != 1 || states[0].Connected || states[0].Error == "" {
		t.Fatalf("states while the daemon is absent = %+v, want one error", states)
	}

	srv, err := rpc.Listen(path, fakeDaemon{})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	waitFor(t, "connection", func() bool { return c.State().Connected })
}

func TestBridgeAllowList(t *testing.T) {
	path, _ := startDaemon(t)
	c := newConn(path, &events{})
	c.Start()
	defer c.Stop()
	waitFor(t, "connection", func() bool { return c.State().Connected })

	b := &Bridge{Conn: c}
	for _, bad := range []string{"browser.activeTab", "version", "echo", "", "usage.summary\n"} {
		if _, err := b.Daemon(bad, nil); err == nil || !strings.Contains(err.Error(), "unknown daemon method") {
			t.Errorf("Daemon(%q) = %v, want it refused", bad, err)
		}
	}
	// An allow-listed method reaches the daemon (which here doesn't define it).
	if _, err := b.Daemon("tracker.status", nil); err == nil || !strings.Contains(err.Error(), "method not found") {
		t.Errorf("tracker.status = %v, want the daemon's answer", err)
	}
}

func TestSaveExportIsExclusiveAndPrivate(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Downloads")
	first, err := SaveExport(dir, "screentime-2026-09-30.csv", []byte("a"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := SaveExport(dir, "screentime-2026-09-30.csv", []byte("b"))
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(second) != "screentime-2026-09-30 (1).csv" {
		t.Fatalf("collision name = %s", second)
	}
	if got, _ := os.ReadFile(first); string(got) != "a" {
		t.Fatalf("first export was overwritten: %q", got)
	}
	info, _ := os.Stat(first)
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %o, want 0600", info.Mode().Perm())
	}
}

func TestSaveExportKeepsToTheBaseName(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "Downloads")
	for _, name := range []string{"../../etc/passwd", "/etc/shadow", `..\..\x`, "a/b/c.csv", "", ".."} {
		path, err := SaveExport(dir, name, []byte("x"))
		if err != nil {
			t.Fatalf("%q: %v", name, err)
		}
		if filepath.Dir(path) != dir {
			t.Errorf("%q escaped to %s", name, path)
		}
	}
}

func TestSaveExportDoesNotFollowASymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(t.TempDir(), "victim")
	if err := os.WriteFile(target, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, "screentime.csv")); err != nil {
		t.Fatal(err)
	}
	path, err := SaveExport(dir, "screentime.csv", []byte("new"))
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(target); string(got) != "keep" {
		t.Fatalf("symlink target was written through: %q", got)
	}
	if filepath.Base(path) != "screentime (1).csv" {
		t.Fatalf("path = %s", path)
	}
}

func status(over func(*ipc.TrackerStatus)) *ipc.TrackerStatus {
	s := &ipc.TrackerStatus{}
	if over != nil {
		over(s)
	}
	return s
}

func actions(items []MenuItem) []string {
	var out []string
	for _, i := range items {
		if !i.Separator {
			out = append(out, i.Action)
		}
	}
	return out
}

func TestTrayMenuOffersPauseAndFocusWhenTracking(t *testing.T) {
	got := strings.Join(actions(BuildTrayMenu(status(nil), true, TrayInfo{})), ",")
	if want := "noop,widget,open,pause:15,pause:60,focus:25,quit"; got != want {
		t.Fatalf("actions = %s, want %s", got, want)
	}
}

func TestTrayMenuResumeAndStop(t *testing.T) {
	paused := actions(BuildTrayMenu(status(func(s *ipc.TrackerStatus) { s.Paused = true }), true, TrayInfo{}))
	if !contains(paused, "resume") || contains(paused, "pause:15") {
		t.Fatalf("paused menu = %v", paused)
	}
	focus := actions(BuildTrayMenu(status(func(s *ipc.TrackerStatus) { s.FocusMode.Active = true }), true, TrayInfo{}))
	if !contains(focus, "focus-stop") {
		t.Fatalf("focus menu = %v", focus)
	}
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

func TestTrayMenuDisablesControlsWhenDaemonIsDown(t *testing.T) {
	disabled := map[string]bool{}
	for _, i := range BuildTrayMenu(nil, false, TrayInfo{}) {
		if !i.Separator {
			disabled[i.Action] = i.Disabled
		}
	}
	if !disabled["pause:15"] || !disabled["focus:25"] || disabled["open"] || disabled["quit"] {
		t.Fatalf("disabled = %v", disabled)
	}
}

func TestTrayStatusLine(t *testing.T) {
	first := func(s *ipc.TrackerStatus, connected bool, info TrayInfo) MenuItem {
		return BuildTrayMenu(s, connected, info)[0]
	}
	cases := []struct {
		s         *ipc.TrackerStatus
		connected bool
		info      TrayInfo
		want      string
	}{
		{status(nil), true, TrayInfo{AppName: "Firefox"}, "Tracking  Firefox"},
		{status(func(s *ipc.TrackerStatus) { s.Paused = true }), true, TrayInfo{ResumeLabel: "15:30"}, "Paused until 15:30"},
		{status(func(s *ipc.TrackerStatus) { s.Idle = true }), true, TrayInfo{}, "Idle"},
		{status(func(s *ipc.TrackerStatus) { s.FocusMode.Active = true }), true, TrayInfo{}, "Focus mode on"},
		{nil, false, TrayInfo{}, "Daemon not running"},
		{nil, true, TrayInfo{}, "Connecting…"},
	}
	for _, c := range cases {
		got := first(c.s, c.connected, c.info)
		if got.Label != c.want || !got.Disabled {
			t.Errorf("status line = %+v, want a disabled %q", got, c.want)
		}
	}
}

func TestTrayTodayRowOnlyWhileConnected(t *testing.T) {
	has := func(connected bool) bool {
		for _, i := range BuildTrayMenu(status(nil), connected, TrayInfo{TodayLabel: "4h 47m"}) {
			if i.Label == "Today  4h 47m" {
				return true
			}
		}
		return false
	}
	if !has(true) || has(false) {
		t.Fatal("the Today row must show only while connected")
	}
}

func TestParseAction(t *testing.T) {
	for _, item := range append(
		BuildTrayMenu(status(nil), true, TrayInfo{}),
		BuildTrayMenu(status(func(s *ipc.TrackerStatus) { s.Paused = true; s.FocusMode.Active = true }), true, TrayInfo{})...,
	) {
		if !item.Separator && !item.Disabled {
			if _, ok := ParseAction(item.Action); !ok {
				t.Errorf("menu action %q does not parse", item.Action)
			}
		}
	}
	if a, ok := ParseAction("pause:15"); !ok || a != (Action{Kind: "pause", Minutes: 15}) {
		t.Errorf("pause:15 = %+v, %v", a, ok)
	}
	for _, bad := range []string{"", "noop", "pause:0", "pause:abc", "pause:-5", "pause:015", "focus:", "rm -rf", "quit:1"} {
		if _, ok := ParseAction(bad); ok {
			t.Errorf("ParseAction(%q) should be rejected", bad)
		}
	}
}

func TestTrayIconAndTitle(t *testing.T) {
	paused := status(func(s *ipc.TrackerStatus) { s.Paused = true })
	focus := status(func(s *ipc.TrackerStatus) { s.FocusMode.Active = true })
	if TrayIcon(nil, false) != "tray-offline" || TrayIcon(paused, true) != "tray-paused" ||
		TrayIcon(focus, true) != "tray-focus" || TrayIcon(status(nil), true) != "tray" {
		t.Fatal("wrong tray icon for a state")
	}
	if !strings.Contains(TrayTitle(nil, false), "not running") || !strings.Contains(TrayTitle(paused, true), "paused") ||
		!strings.Contains(TrayTitle(status(func(s *ipc.TrackerStatus) { s.Idle = true }), true), "idle") ||
		!strings.Contains(TrayTitle(status(nil), true), "tracking") {
		t.Fatal("wrong tray title for a state")
	}
}

func TestShortDuration(t *testing.T) {
	for ms, want := range map[int64]string{0: "0m", 29_000: "0m", 90_000: "2m", 3_600_000: "1h 00m", 17_220_000: "4h 47m"} {
		if got := ShortDuration(ms); got != want {
			t.Errorf("ShortDuration(%d) = %s, want %s", ms, got, want)
		}
	}
}
