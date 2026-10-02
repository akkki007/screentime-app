package rules

import (
	"database/sql"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/akkki007/screentime-app/internal/apps"
	"github.com/akkki007/screentime-app/internal/ipc"
	"github.com/akkki007/screentime-app/internal/queries"
	"github.com/akkki007/screentime-app/internal/store"
	"github.com/akkki007/screentime-app/internal/tracker"
)

const (
	minute = int64(60_000)
	hour   = 60 * minute
)

type note struct{ title, body string }

type recordingNotifier struct{ notes *[]note }

func (r recordingNotifier) Notify(title, body string) { *r.notes = append(*r.notes, note{title, body}) }

// harness is the TS beforeEach: a fresh database, a fake clock starting at
// Mon 2026-01-05 10:00 local and recorders for everything the engine emits.
type harness struct {
	t         *testing.T
	db        *sql.DB
	start     int64
	clock     int64
	settings  ipc.Settings
	state     tracker.State
	domain    string
	notes     []note
	hits      []ipc.LimitHitEvent
	reminders []ipc.ReminderEvent
	engine    *Engine
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatal(err)
	}
	prev := time.Local
	time.Local = loc
	t.Cleanup(func() { time.Local = prev })

	db, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	h := &harness{t: t, db: db, settings: ipc.DefaultSettings}
	h.start = h.at(2026, 1, 5, 10, 0)
	h.clock = h.start
	h.engine = New(Deps{
		DB:           db,
		Notifier:     recordingNotifier{&h.notes},
		Now:          func() int64 { return h.clock },
		Settings:     func() ipc.Settings { return h.settings },
		State:        func() tracker.State { return h.state },
		ActiveDomain: func() string { return h.domain },
		LimitHit:     func(e ipc.LimitHitEvent) { h.hits = append(h.hits, e) },
		Reminder:     func(e ipc.ReminderEvent) { h.reminders = append(h.reminders, e) },
	})
	return h
}

// at is a local wall-clock time in milliseconds.
func (h *harness) at(y int, mo time.Month, d, hh, mm int) int64 {
	return time.Date(y, mo, d, hh, mm, 0, 0, time.Local).UnixMilli()
}

func (h *harness) addSession(appID string, from, to int64) {
	h.t.Helper()
	id, err := apps.Ensure(h.db, appID, func(string) (apps.Entry, bool) { return apps.Entry{}, false })
	if err != nil {
		h.t.Fatal(err)
	}
	if _, err := h.db.Exec("INSERT INTO sessions (app_id, start_ts, end_ts, source) VALUES (?, ?, ?, 'desktop')",
		id, from, to); err != nil {
		h.t.Fatal(err)
	}
}

func (h *harness) setLimit(l ipc.Limit) ipc.Limit {
	h.t.Helper()
	got, err := queries.SetLimit(h.db, l)
	if err != nil {
		h.t.Fatal(err)
	}
	return got
}

// run advances the clock in 5 s heartbeats, ticking the engine each time.
func (h *harness) run(ms int64) {
	for t := int64(0); t < ms; t += 5_000 {
		h.clock += 5_000
		h.engine.Tick()
	}
}

func (h *harness) wantHits(n int) {
	h.t.Helper()
	if len(h.hits) != n {
		h.t.Fatalf("hits = %v, want %d", h.hits, n)
	}
}

func (h *harness) wantReminders(n int) {
	h.t.Helper()
	if len(h.reminders) != n {
		h.t.Fatalf("reminders = %v, want %d", h.reminders, n)
	}
}

// --- daily limits -----------------------------------------------------------

func TestLimitFiresOnceWhenUsageCrossesIt(t *testing.T) {
	h := newHarness(t)
	limit := h.setLimit(ipc.Limit{TargetType: "app", Target: "org.mozilla.firefox", DailyMs: hour, Action: "notify"})
	h.addSession("org.mozilla.firefox", h.start-59*minute, h.start)
	h.engine.Tick()
	h.wantHits(0)

	h.addSession("org.mozilla.firefox", h.start, h.start+2*minute)
	h.clock += 2 * minute
	h.engine.Tick()
	if want := []ipc.LimitHitEvent{{LimitID: *limit.ID, Action: "notify"}}; !reflect.DeepEqual(h.hits, want) {
		t.Fatalf("hits = %v, want %v", h.hits, want)
	}
	if len(h.notes) != 1 {
		t.Fatalf("notes = %v, want 1", h.notes)
	}
	if !strings.Contains(h.notes[0].body, "1 h") {
		t.Errorf("note body = %q, want it to contain %q", h.notes[0].body, "1 h")
	}

	h.run(30 * minute)
	h.wantHits(1)
}

func TestLimitCountsOnlyTheSessionPartInsideToday(t *testing.T) {
	h := newHarness(t)
	h.setLimit(ipc.Limit{TargetType: "app", Target: "a", DailyMs: hour, Action: "notify"})
	// A session that began yesterday evening: only its 30 minutes after midnight count today.
	midnight := h.at(2026, 1, 5, 0, 0)
	h.addSession("a", midnight-5*hour, midnight+30*minute)
	h.engine.Tick()
	h.wantHits(0)
}

func TestLimitFiresAgainTheNextDay(t *testing.T) {
	h := newHarness(t)
	h.setLimit(ipc.Limit{TargetType: "app", Target: "a", DailyMs: hour, Action: "notify"})
	h.addSession("a", h.start-2*hour, h.start)
	h.engine.Tick()
	h.wantHits(1)

	tomorrow := h.at(2026, 1, 6, 10, 0)
	h.addSession("a", tomorrow-2*hour, tomorrow)
	h.clock = tomorrow
	h.engine.Tick()
	h.wantHits(2)
}

func TestOverlayLimitsRepeatWhileInUseNotifyLimitsDoNot(t *testing.T) {
	h := newHarness(t)
	h.setLimit(ipc.Limit{TargetType: "app", Target: "a", DailyMs: hour, Action: "overlay"})
	h.setLimit(ipc.Limit{TargetType: "app", Target: "b", DailyMs: hour, Action: "notify"})
	h.addSession("a", h.start-2*hour, h.start)
	h.addSession("b", h.start-2*hour, h.start)
	h.state.CurrentAppID = "a"
	h.engine.Tick()
	h.wantHits(2)

	h.run(4 * minute)
	h.wantHits(2) // inside the repeat window
	h.run(2 * minute)
	var actions []string
	for _, hit := range h.hits {
		actions = append(actions, hit.Action)
	}
	if want := []string{"overlay", "notify", "overlay"}; !reflect.DeepEqual(actions, want) {
		t.Fatalf("actions = %v, want %v", actions, want)
	}

	h.state.CurrentAppID = "other"
	h.run(10 * minute)
	h.wantHits(3) // not in use: stays quiet
}

func TestLimitCategoryAndDomainTargets(t *testing.T) {
	h := newHarness(t)
	h.setLimit(ipc.Limit{TargetType: "category", Target: "Browsing", DailyMs: hour, Action: "notify"})
	h.setLimit(ipc.Limit{TargetType: "domain", Target: "youtube.com", DailyMs: 30 * minute, Action: "notify"})
	h.addSession("org.mozilla.firefox", h.start-2*hour, h.start)
	if _, err := h.db.Exec("INSERT INTO web_sessions (domain, start_ts, end_ts) VALUES (?, ?, ?)",
		"youtube.com", h.start-hour, h.start); err != nil {
		t.Fatal(err)
	}
	h.engine.Tick()
	h.wantHits(2)
}

func TestLimitScheduleRestrictsEnforcementToItsWindow(t *testing.T) {
	h := newHarness(t)
	// Window 13:00-17:00 while it's 10:00
	h.setLimit(ipc.Limit{TargetType: "app", Target: "a", DailyMs: hour, Action: "notify", Schedule: "13:00-17:00"})
	h.addSession("a", h.start-2*hour, h.start)
	h.engine.Tick()
	h.wantHits(0)

	h.clock = h.at(2026, 1, 5, 13, 30)
	h.engine.Tick()
	h.wantHits(1)
}

// --- break reminders --------------------------------------------------------

func newBreakHarness(t *testing.T) *harness {
	h := newHarness(t)
	h.settings.BreakRemindersEnabled = true
	h.settings.BreakEveryMinutes = 50
	h.settings.BreakLengthMinutes = 5
	return h
}

func TestBreakReminderAfterContinuousActivityThenEveryTenMinutes(t *testing.T) {
	h := newBreakHarness(t)
	h.run(49 * minute)
	h.wantReminders(0)
	h.run(2 * minute)
	h.wantReminders(1)
	if h.reminders[0].Kind != "break" {
		t.Errorf("kind = %q, want break", h.reminders[0].Kind)
	}
	if len(h.notes) == 0 || !strings.Contains(h.notes[0].body, "50 min") {
		t.Errorf("notes = %v, want the first to contain %q", h.notes, "50 min")
	}

	h.run(8 * minute)
	h.wantReminders(1)
	h.run(3 * minute)
	h.wantReminders(2)
}

func TestBreakRealBreakResetsTheClockShortOneDoesNot(t *testing.T) {
	h := newBreakHarness(t)
	h.run(40 * minute)
	h.engine.OnTrackerEvent(tracker.Event{Type: tracker.EventIdle, At: h.clock})
	h.clock += 2 * minute // too short
	h.engine.OnTrackerEvent(tracker.Event{Type: tracker.EventActive, At: h.clock})
	h.run(9 * minute)
	h.wantReminders(1) // 40 + 2 + 9 = 51 min since the start

	h.reminders = nil
	h.engine.OnTrackerEvent(tracker.Event{Type: tracker.EventIdle, At: h.clock})
	h.clock += 6 * minute // a proper break
	h.engine.OnTrackerEvent(tracker.Event{Type: tracker.EventActive, At: h.clock})
	h.run(40 * minute)
	h.wantReminders(0)
}

func TestBreakSuspendCountsAsABreak(t *testing.T) {
	h := newBreakHarness(t)
	h.run(45 * minute)
	h.clock += 3 * hour // laptop asleep
	h.engine.Tick()
	h.run(40 * minute)
	h.wantReminders(0)
}

func TestBreakQuietWhileIdlePausedOrDisabled(t *testing.T) {
	h := newBreakHarness(t)
	h.state.Idle = true
	h.run(2 * hour)
	h.wantReminders(0)

	h.state.Idle = false
	h.settings.BreakRemindersEnabled = false
	h.run(2 * hour)
	h.wantReminders(0)
}

// --- downtime ---------------------------------------------------------------

func newDowntimeHarness(t *testing.T) *harness {
	h := newHarness(t)
	h.settings.DowntimeEnabled = true
	h.settings.DowntimeStart = "22:00"
	h.settings.DowntimeEnd = "07:00"
	h.state.CurrentAppID = "a"
	return h
}

func TestDowntimeNudgesInsideWrappingWindowEveryFifteenMinutes(t *testing.T) {
	h := newDowntimeHarness(t)
	h.clock = h.at(2026, 1, 5, 23, 0)
	h.engine.Tick()
	h.wantReminders(1)
	if r := h.reminders[0]; r.Kind != "downtime" || !strings.Contains(r.Message, "07:00") {
		t.Fatalf("reminder = %+v, want kind downtime mentioning 07:00", r)
	}
	h.run(10 * minute)
	h.wantReminders(1)
	h.run(6 * minute)
	h.wantReminders(2)
}

func TestDowntimeSilentOutsideWindowAndWhenNobodyIsUsingTheComputer(t *testing.T) {
	h := newDowntimeHarness(t)
	h.engine.Tick() // 10:00
	h.wantReminders(0)

	h.clock = h.at(2026, 1, 5, 23, 0)
	h.state.CurrentAppID = ""
	h.engine.Tick()
	h.wantReminders(0)
}

// --- focus mode -------------------------------------------------------------

func TestFocusModeNudgesRateLimitedAndAnnouncesTheEnd(t *testing.T) {
	h := newHarness(t)
	h.addSession("com.spotify.Client", h.start-minute, h.start) // categorised as Entertainment (distracting)
	until := h.engine.StartFocusMode(25)
	if until != h.start+25*minute {
		t.Fatalf("until = %d, want %d", until, h.start+25*minute)
	}
	if got := h.engine.FocusMode(); !got.Active || got.Until == nil || *got.Until != until {
		t.Fatalf("FocusMode = %+v, want active until %d", got, until)
	}

	h.engine.OnTrackerEvent(tracker.Event{Type: tracker.EventFocus, AppID: "org.gnome.Ptyxis", At: h.clock}) // productive: fine
	h.wantReminders(0)

	h.engine.OnTrackerEvent(tracker.Event{Type: tracker.EventFocus, AppID: "com.spotify.Client", At: h.clock})
	h.wantReminders(1)
	h.engine.OnTrackerEvent(tracker.Event{Type: tracker.EventFocus, AppID: "com.spotify.Client", At: h.clock})
	h.wantReminders(1) // within the minute

	h.run(25 * minute)
	if h.engine.FocusMode().Active {
		t.Error("focus mode still active after it expired")
	}
	if last := h.reminders[len(h.reminders)-1]; !strings.Contains(last.Message, "finished") {
		t.Errorf("last reminder = %q, want it to contain %q", last.Message, "finished")
	}
}

func TestFocusModeOffDoesNothing(t *testing.T) {
	h := newHarness(t)
	h.addSession("com.spotify.Client", h.start-minute, h.start)
	h.engine.OnTrackerEvent(tracker.Event{Type: tracker.EventFocus, AppID: "com.spotify.Client", At: h.clock})
	h.wantReminders(0)
}

func TestFormatDuration(t *testing.T) {
	for _, c := range []struct {
		ms   int64
		want string
	}{
		{30 * minute, "30 min"},
		{hour, "1 h"},
		{hour + 5*minute, "1 h 5 min"},
	} {
		if got := FormatDuration(c.ms); got != c.want {
			t.Errorf("FormatDuration(%d) = %q, want %q", c.ms, got, c.want)
		}
	}
}
