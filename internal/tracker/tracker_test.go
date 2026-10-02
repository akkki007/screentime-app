package tracker

import (
	"context"
	"database/sql"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/akkki007/screentime-app/internal/focus"
	"github.com/akkki007/screentime-app/internal/store"
)

const threshold = 180_000

// fakeProvider captures the tracker's callbacks so tests can fire them.
type fakeProvider struct {
	onFocus func(focus.Window)
	onIdle  func(bool)
}

func (*fakeProvider) ID() string                     { return "fake" }
func (*fakeProvider) Available(context.Context) bool { return true }
func (p *fakeProvider) OnFocusChange(cb func(focus.Window)) func() {
	p.onFocus = cb
	return func() {}
}
func (p *fakeProvider) OnIdleChange(cb func(bool), _ int64) func() {
	p.onIdle = cb
	return func() {}
}

type row struct {
	appID   string
	title   *string
	startTs int64
	endTs   int64
}

// harness is a tracker wired to a fake provider and a manually advanced clock.
type harness struct {
	t        *testing.T
	db       *sql.DB
	tracker  *Tracker
	provider *fakeProvider
	clock    int64
}

func openDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func setup(t *testing.T, captureTitles bool) *harness {
	t.Helper()
	h := &harness{t: t, db: openDB(t), provider: &fakeProvider{}, clock: 1_000_000}
	h.tracker = New(h.db, h.provider, Options{
		IdleThresholdMs: threshold,
		CaptureTitles:   captureTitles,
		Now:             func() int64 { return h.clock },
	})
	h.tracker.Start()
	t.Cleanup(h.tracker.Stop)
	return h
}

func (h *harness) now() int64         { return h.clock }
func (h *harness) advance(ms int64)   { h.clock += ms }
func (h *harness) idle(idle bool)     { h.provider.onIdle(idle) }
func (h *harness) beat()              { h.tracker.Heartbeat() }
func (h *harness) focus(appID string) { h.focusTitle(appID, "") }
func (h *harness) focusTitle(appID, title string) {
	h.provider.onFocus(focus.Window{AppID: appID, Title: title, Ts: h.clock})
}

func (h *harness) rows() []row {
	h.t.Helper()
	rs, err := h.db.Query("SELECT apps.app_id, title, start_ts, end_ts FROM sessions JOIN apps ON apps.id = sessions.app_id ORDER BY sessions.id")
	if err != nil {
		h.t.Fatal(err)
	}
	defer rs.Close()
	var out []row
	for rs.Next() {
		var r row
		if err := rs.Scan(&r.appID, &r.title, &r.startTs, &r.endTs); err != nil {
			h.t.Fatal(err)
		}
		out = append(out, r)
	}
	if err := rs.Err(); err != nil {
		h.t.Fatal(err)
	}
	return out
}

// titles maps rows to their titles, "<nil>" standing for NULL.
func (h *harness) titles() []string {
	var out []string
	for _, r := range h.rows() {
		if r.title == nil {
			out = append(out, "<nil>")
		} else {
			out = append(out, *r.title)
		}
	}
	return out
}

func (h *harness) wantRows(n int) []row {
	h.t.Helper()
	rows := h.rows()
	if len(rows) != n {
		h.t.Fatalf("got %d rows %+v, want %d", len(rows), rows, n)
	}
	return rows
}

func ptrEq(p *int64, v int64) bool { return p != nil && *p == v }

func TestWindowTitles(t *testing.T) {
	t.Run("are not stored by default", func(t *testing.T) {
		h := setup(t, false)
		h.focusTitle("org.mozilla.firefox", "secret bank page")
		h.advance(1_000)
		h.focusTitle("org.mozilla.firefox", "another secret")
		if got, want := h.titles(), []string{"<nil>"}; !reflect.DeepEqual(got, want) {
			t.Errorf("titles = %v, want %v", got, want)
		}
	})

	t.Run("are stored when the user opts in", func(t *testing.T) {
		h := setup(t, true)
		h.focusTitle("org.mozilla.firefox", "docs")
		if got, want := h.titles(), []string{"docs"}; !reflect.DeepEqual(got, want) {
			t.Errorf("titles = %v, want %v", got, want)
		}
	})
}

func TestAppIdentity(t *testing.T) {
	t.Run("strips the .desktop suffix", func(t *testing.T) {
		for in, want := range map[string]string{
			"org.mozilla.firefox.desktop": "org.mozilla.firefox",
			"Xterm":                       "Xterm",
			// GNOME's synthetic per-window IDs must not each become their own "app".
			"window:28":      "unknown",
			"window:7":       "unknown",
			"window:manager": "window:manager",
		} {
			if got := NormalizeAppID(in); got != want {
				t.Errorf("NormalizeAppID(%q) = %q, want %q", in, got, want)
			}
		}
		h := setup(t, false)
		h.focus("org.gnome.Ptyxis.desktop")
		if got := h.wantRows(1)[0].appID; got != "org.gnome.Ptyxis" {
			t.Errorf("app_id = %q, want org.gnome.Ptyxis", got)
		}
	})
}

func TestMergeRule(t *testing.T) {
	t.Run("same app within the gap extends one session", func(t *testing.T) {
		h := setup(t, false)
		h.focus("a")
		h.advance(5_000)
		h.focus("a")
		if got := h.wantRows(1)[0].endTs; got != h.now() {
			t.Errorf("end_ts = %d, want %d", got, h.now())
		}
	})

	t.Run("switching apps closes the old session and opens a new one", func(t *testing.T) {
		h := setup(t, false)
		start := h.now()
		h.focus("a")
		h.advance(4_000)
		h.beat()
		h.advance(1_000)
		h.focus("b")
		rows := h.wantRows(2)
		// ends at the switch (5 s in), not at the earlier 4 s heartbeat
		if a := rows[0]; a.appID != "a" || a.startTs != start || a.endTs != start+5_000 {
			t.Errorf("a = %+v, want a [%d, %d]", a, start, start+5_000)
		}
		if b := rows[1]; b.appID != "b" || b.startTs != h.now() {
			t.Errorf("b = %+v, want b starting %d", b, h.now())
		}
	})
}

func TestIdle(t *testing.T) {
	t.Run("ends the session at the start of the idle period", func(t *testing.T) {
		h := setup(t, false)
		start := h.now()
		h.focus("a")
		// 1 minute of real use, then 3 minutes idle while heartbeats keep ticking.
		for range 48 {
			h.advance(5_000)
			h.beat()
		}
		h.idle(true) // fires 4 minutes in; idle began 3 minutes ago = 1 minute in
		if got, want := h.wantRows(1)[0].endTs, start+4*60_000-threshold; got != want {
			t.Errorf("end_ts = %d, want %d", got, want)
		}
	})

	t.Run("resumes tracking the same window when activity returns", func(t *testing.T) {
		h := setup(t, false)
		h.focus("a")
		h.advance(threshold)
		h.idle(true)
		h.advance(60_000)
		h.idle(false) // no focus event: the user just touched the mouse
		if r := h.wantRows(2)[1]; r.appID != "a" || r.startTs != h.now() {
			t.Errorf("row = %+v, want a starting %d", r, h.now())
		}
	})

	t.Run("focus changes while idle are remembered but not tracked", func(t *testing.T) {
		h := setup(t, false)
		h.focus("a")
		h.advance(threshold)
		h.idle(true)
		h.focus("b")
		h.wantRows(1)
		h.idle(false)
		if got := h.wantRows(2)[1].appID; got != "b" {
			t.Errorf("app_id = %q, want b", got)
		}
	})
}

func TestSuspend(t *testing.T) {
	t.Run("a long heartbeat gap splits the session instead of stretching it", func(t *testing.T) {
		h := setup(t, false)
		start := h.now()
		h.focus("a")
		h.advance(5_000)
		h.beat()
		h.advance(8 * 3_600_000) // laptop asleep for 8 hours
		h.beat()
		rows := h.wantRows(2)
		if before := rows[0]; before.startTs != start || before.endTs != start+5_000 {
			t.Errorf("before = %+v, want [%d, %d]", before, start, start+5_000)
		}
		if got := rows[1].startTs; got != h.now() {
			t.Errorf("after start_ts = %d, want %d", got, h.now())
		}
	})
}

func TestPause(t *testing.T) {
	t.Run("stops counting, remembers focus, and resumes on the first heartbeat after expiry", func(t *testing.T) {
		h := setup(t, false)
		h.focus("a")
		h.advance(5_000)
		h.beat()
		until := h.tracker.Pause(10)
		if want := h.now() + 10*60_000; until != want {
			t.Errorf("Pause = %d, want %d", until, want)
		}
		if s := h.tracker.State(); !s.Paused || !ptrEq(s.ResumeAt, until) || s.CurrentAppID != "" {
			t.Errorf("state = %+v, want paused until %d with no current app", s, until)
		}

		h.advance(60_000)
		h.focus("b") // switching while paused is remembered but not tracked
		h.wantRows(1)

		h.advance(10 * 60_000)
		h.beat()
		if r := h.wantRows(2)[1]; r.appID != "b" || r.startTs != h.now() {
			t.Errorf("row = %+v, want b starting %d", r, h.now())
		}
		if h.tracker.State().Paused {
			t.Error("still paused")
		}
	})

	t.Run("resume() ends a pause early", func(t *testing.T) {
		h := setup(t, false)
		h.focus("a")
		h.tracker.Pause(30)
		h.advance(1_000)
		h.tracker.Resume()
		h.wantRows(2)
		if s := h.tracker.State(); s.Paused || s.CurrentAppID != "a" {
			t.Errorf("state = %+v, want unpaused on a", s)
		}
	})
}

func TestEventsAndState(t *testing.T) {
	t.Run("emits focus, idle, active and pause events", func(t *testing.T) {
		h := setup(t, false)
		var events []EventType
		h.tracker.OnEvent(func(e Event) { events = append(events, e.Type) })

		h.focus("a")
		h.focus("a") // same app: no second event
		h.advance(threshold)
		h.idle(true)
		h.idle(false)
		h.tracker.Pause(5)
		h.tracker.Resume()

		want := []EventType{EventFocus, EventIdle, EventActive, EventPaused, EventResumed}
		if !reflect.DeepEqual(events, want) {
			t.Errorf("events = %v, want %v", events, want)
		}
	})

	t.Run("idle event carries the time idleness began", func(t *testing.T) {
		h := setup(t, false)
		var at int64
		h.tracker.OnEvent(func(e Event) {
			if e.Type == EventIdle {
				at = e.At
			}
		})
		h.focus("a")
		h.advance(threshold)
		h.idle(true)
		if want := h.now() - threshold; at != want {
			t.Errorf("idle at = %d, want %d", at, want)
		}
	})

	t.Run("state() reports the counted app only while tracking", func(t *testing.T) {
		h := setup(t, false)
		if got := h.tracker.State().CurrentAppID; got != "" {
			t.Errorf("CurrentAppID = %q before any focus", got)
		}
		h.focus("a")
		if s := h.tracker.State(); s.CurrentAppID != "a" || !ptrEq(s.Since, h.now()) || s.Idle {
			t.Errorf("state = %+v, want a since %d, not idle", s, h.now())
		}
		h.advance(threshold)
		h.idle(true)
		if s := h.tracker.State(); s.CurrentAppID != "" || !s.Idle {
			t.Errorf("state = %+v, want idle with no current app", s)
		}
	})
}

func TestConfigure(t *testing.T) {
	t.Run("toggling captureTitles takes effect for later sessions", func(t *testing.T) {
		h := setup(t, false)
		h.focusTitle("a", "private")
		h.tracker.Configure(true, threshold)
		h.advance(20_000)
		h.focusTitle("b", "shared")
		if got, want := h.titles(), []string{"<nil>", "shared"}; !reflect.DeepEqual(got, want) {
			t.Errorf("titles = %v, want %v", got, want)
		}
	})
}

// Not in the TypeScript suite: after data.wipe deletes the rows under the
// open session, tracking carries on in a fresh row from the last focus.
func TestRestartSessionAfterWipe(t *testing.T) {
	h := setup(t, false)
	h.focus("org.mozilla.firefox")
	h.advance(HeartbeatMs)
	h.beat()
	if _, err := h.db.Exec("DELETE FROM sessions"); err != nil {
		t.Fatal(err)
	}
	h.tracker.RestartSession()
	start := h.clock
	h.advance(HeartbeatMs)
	h.beat()
	rows := h.wantRows(1)
	if rows[0].appID != "org.mozilla.firefox" || rows[0].startTs != start || rows[0].endTs != start+HeartbeatMs {
		t.Errorf("row = %+v, want firefox from %d to %d", rows[0], start, start+HeartbeatMs)
	}

	// While paused, a restart opens nothing.
	h.tracker.Pause(15)
	h.db.Exec("DELETE FROM sessions")
	h.tracker.RestartSession()
	h.wantRows(0)
}
