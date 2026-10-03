// Package tracker turns focus and idle changes into `sessions` rows, and
// browser reports into `web_sessions`. Port of apps/daemon/src/tracker.ts
// and web-tracker.ts.
//
// Neither type is safe for concurrent use: the daemon serialises every call
// (provider callbacks, the heartbeat and RPC handlers) behind one lock, as
// JavaScript's event loop did for the Bun daemon.
package tracker

import (
	"database/sql"
	"log"
	"regexp"
	"strings"
	"time"

	"github.com/akkki007/screentime-app/internal/apps"
	"github.com/akkki007/screentime-app/internal/focus"
)

const (
	// HeartbeatMs is how often the open session's end is extended.
	HeartbeatMs = 5_000
	// mergeGapMs: a gap shorter than this between sightings of the same app
	// extends the open session instead of starting a new one.
	mergeGapMs             = 10_000
	defaultIdleThresholdMs = 3 * 60 * 1_000
)

// EventType says what changed.
type EventType int

const (
	EventFocus EventType = iota
	EventIdle
	EventActive
	EventPaused
	EventResumed
)

// Event is a state change, for the rules engine and RPC notifications.
type Event struct {
	Type  EventType
	AppID string // EventFocus
	At    int64  // EventFocus (since), EventIdle, EventActive
	Until int64  // EventPaused
}

// State is what tracker.status reports.
type State struct {
	Paused   bool
	ResumeAt *int64
	Idle     bool
	// CurrentAppID is the app being counted; empty while idle, paused or unfocused.
	CurrentAppID string
	Since        *int64
}

// Options configures a Tracker.
type Options struct {
	IdleThresholdMs int64
	// CaptureTitles keeps window titles; they often contain private data.
	CaptureTitles bool
	Now           func() int64
	Lookup        apps.Lookup
	// Serialize runs provider callbacks under the daemon's lock.
	Serialize func(func())
	// Debug receives log lines; never titles unless CaptureTitles is on.
	Debug func(string)
}

type openSession struct {
	rowID      int64
	appID      string
	startTs    int64
	lastSeenTs int64
}

// Tracker turns focus/idle events into sessions.
//
// Merge rule: same app, gap under mergeGapMs and not idle extends the open
// row's end_ts in place; anything else closes it and opens a new one. The
// heartbeat re-extends end_ts every HeartbeatMs, so a crash loses at most
// one interval.
type Tracker struct {
	db       *sql.DB
	provider focus.Provider
	opts     Options

	open        *openSession
	current     *focus.Window // latest focus, to resume after idle, pause or suspend
	idle        bool
	pausedUntil *int64
	listeners   []func(Event)

	unsubscribeFocus, unsubscribeIdle func()
}

// New creates a tracker; call Start to subscribe to the provider.
func New(db *sql.DB, provider focus.Provider, opts Options) *Tracker {
	if opts.IdleThresholdMs == 0 {
		opts.IdleThresholdMs = defaultIdleThresholdMs
	}
	if opts.Now == nil {
		opts.Now = func() int64 { return time.Now().UnixMilli() }
	}
	if opts.Serialize == nil {
		opts.Serialize = func(f func()) { f() }
	}
	if opts.Debug == nil {
		opts.Debug = func(string) {}
	}
	if opts.Lookup == nil {
		opts.Lookup = func(string) (apps.Entry, bool) { return apps.Entry{}, false }
	}
	return &Tracker{db: db, provider: provider, opts: opts}
}

// Start subscribes to focus and idle changes. The daemon drives Heartbeat.
func (t *Tracker) Start() {
	t.passTitleSetting()
	t.unsubscribeFocus = t.provider.OnFocusChange(func(w focus.Window) {
		t.opts.Serialize(func() { t.HandleFocusChange(w) })
	})
	t.subscribeIdle()
}

// Stop unsubscribes and closes the open session.
func (t *Tracker) Stop() {
	if t.unsubscribeFocus != nil {
		t.unsubscribeFocus()
	}
	if t.unsubscribeIdle != nil {
		t.unsubscribeIdle()
	}
	t.closeOpenSession(nil)
}

// OnEvent registers a listener, called synchronously on every change.
func (t *Tracker) OnEvent(listener func(Event)) { t.listeners = append(t.listeners, listener) }

func (t *Tracker) emit(e Event) {
	for _, l := range t.listeners {
		l(e)
	}
}

// State reports the current state.
func (t *Tracker) State() State {
	s := State{Paused: t.pausedUntil != nil, ResumeAt: t.pausedUntil, Idle: t.idle}
	if t.open != nil {
		s.CurrentAppID = t.open.appID
		since := t.open.startTs
		s.Since = &since
	}
	return s
}

// Configure applies changed settings without dropping the focus subscription.
func (t *Tracker) Configure(captureTitles bool, idleThresholdMs int64) {
	if captureTitles != t.opts.CaptureTitles {
		t.opts.CaptureTitles = captureTitles
		t.passTitleSetting()
	}
	if idleThresholdMs != t.opts.IdleThresholdMs {
		t.opts.IdleThresholdMs = idleThresholdMs
		// Changing a setting needs the user at the keyboard, so they're not idle.
		t.HandleIdleChange(false)
		if t.unsubscribeIdle != nil {
			t.unsubscribeIdle()
		}
		t.subscribeIdle()
	}
}

// passTitleSetting lets a provider that supports it drop titles at the
// source; the tracker drops them as well either way.
func (t *Tracker) passTitleSetting() {
	if tc, ok := t.provider.(focus.TitleCapture); ok {
		tc.SetCaptureTitles(t.opts.CaptureTitles)
	}
}

func (t *Tracker) subscribeIdle() {
	t.unsubscribeIdle = t.provider.OnIdleChange(func(idle bool) {
		t.opts.Serialize(func() { t.HandleIdleChange(idle) })
	}, t.opts.IdleThresholdMs)
}

// Pause stops counting time for the given minutes and returns when it resumes.
func (t *Tracker) Pause(minutes int64) int64 {
	until := t.opts.Now() + minutes*60_000
	t.pausedUntil = &until
	t.closeOpenSession(nil)
	t.opts.Debug("paused until " + time.UnixMilli(until).UTC().Format(time.RFC3339))
	t.emit(Event{Type: EventPaused, Until: until})
	return until
}

// Resume starts counting again; a no-op when not paused.
func (t *Tracker) Resume() {
	if t.pausedUntil == nil {
		return
	}
	t.pausedUntil = nil
	t.opts.Debug("resumed")
	if !t.idle && t.current != nil {
		w := *t.current
		w.Ts = t.opts.Now()
		t.openSessionFor(w)
	}
	t.emit(Event{Type: EventResumed})
}

// RestartSession drops the open session without writing it and starts a
// fresh one from the last known focus, after the rows under it were wiped.
func (t *Tracker) RestartSession() {
	t.open = nil
	if !t.idle && t.pausedUntil == nil && t.current != nil {
		w := *t.current
		w.Ts = t.opts.Now()
		t.openSessionFor(w)
	}
}

// HandleFocusChange records a focus change.
func (t *Tracker) HandleFocusChange(raw focus.Window) {
	w := raw
	w.AppID = NormalizeAppID(raw.AppID)
	if !t.opts.CaptureTitles {
		w.Title = ""
	}
	appChanged := t.current == nil || t.current.AppID != w.AppID
	t.current = &w
	t.opts.Debug(describeFocus(w, t.idle))
	if appChanged {
		t.emit(Event{Type: EventFocus, AppID: w.AppID, At: w.Ts})
	}
	if t.idle || t.pausedUntil != nil {
		return
	}
	if t.open != nil && t.open.appID == w.AppID && w.Ts-t.open.lastSeenTs < mergeGapMs {
		t.touch(w.Ts)
		return
	}
	// The user was in the old app right up to this switch, so end it here
	// rather than at the last heartbeat, unless the gap is so long that the
	// machine must have been suspended.
	if t.open != nil && w.Ts-t.open.lastSeenTs < mergeGapMs {
		t.touch(w.Ts)
	}
	t.closeOpenSession(nil)
	t.openSessionFor(w)
}

func describeFocus(w focus.Window, idle bool) string {
	var b strings.Builder
	b.WriteString("focus -> " + w.AppID)
	if w.Title != "" {
		b.WriteString(` "` + w.Title + `"`)
	}
	if idle {
		b.WriteString(" (idle)")
	}
	return b.String()
}

// HandleIdleChange records the user going idle or coming back.
func (t *Tracker) HandleIdleChange(idle bool) {
	if idle == t.idle {
		return
	}
	t.idle = idle
	now := t.opts.Now()
	if idle {
		t.opts.Debug("idle")
		// The idle watch fires after the threshold, but the heartbeat kept
		// extending the session meanwhile: idleness began a threshold ago.
		since := now - t.opts.IdleThresholdMs
		t.closeOpenSession(&since)
		t.emit(Event{Type: EventIdle, At: since})
		return
	}
	t.opts.Debug("active")
	// Returning to the same window sends no focus event, so resume from it.
	if t.current != nil && t.pausedUntil == nil {
		w := *t.current
		w.Ts = now
		t.openSessionFor(w)
	}
	t.emit(Event{Type: EventActive, At: now})
}

// Heartbeat extends the open session; call it every HeartbeatMs.
func (t *Tracker) Heartbeat() {
	now := t.opts.Now()
	// Checked here rather than with a timer, so an expiry that passes during
	// suspend is still honoured on wake.
	if t.pausedUntil != nil && now >= *t.pausedUntil {
		t.Resume()
	}
	if t.open == nil || t.idle {
		return
	}
	if now-t.open.lastSeenTs > mergeGapMs {
		// Heartbeats stopped for far longer than their interval: the machine
		// was suspended. End where we last saw activity and start afresh.
		t.opts.Debug("gap detected (suspend?), splitting session")
		t.closeOpenSession(nil)
		if t.current != nil {
			w := *t.current
			w.Ts = now
			t.openSessionFor(w)
		}
		return
	}
	t.touch(now)
}

func (t *Tracker) openSessionFor(w focus.Window) {
	appRow, err := apps.Ensure(t.db, w.AppID, t.opts.Lookup)
	if err != nil {
		log.Printf("[tracker] recording %s: %v", w.AppID, err)
		return
	}
	var title any
	if w.Title != "" {
		title = w.Title
	}
	res, err := t.db.Exec(
		"INSERT INTO sessions (app_id, title, start_ts, end_ts, source) VALUES (?, ?, ?, ?, 'desktop')",
		appRow, title, w.Ts, w.Ts,
	)
	if err != nil {
		log.Printf("[tracker] opening session: %v", err)
		return
	}
	id, _ := res.LastInsertId()
	t.open = &openSession{rowID: id, appID: w.AppID, startTs: w.Ts, lastSeenTs: w.Ts}
}

func (t *Tracker) touch(ts int64) {
	if t.open == nil {
		return
	}
	t.open.lastSeenTs = ts
	if _, err := t.db.Exec("UPDATE sessions SET end_ts = ? WHERE id = ?", ts, t.open.rowID); err != nil {
		log.Printf("[tracker] extending session: %v", err)
	}
}

// closeOpenSession ends the open session at its last sighting, or earlier at
// endTs, but never before it started.
func (t *Tracker) closeOpenSession(endTs *int64) {
	if t.open == nil {
		return
	}
	end := t.open.lastSeenTs
	if endTs != nil {
		end = min(end, *endTs)
	}
	end = max(t.open.startTs, end)
	if _, err := t.db.Exec("UPDATE sessions SET end_ts = ? WHERE id = ?", end, t.open.rowID); err != nil {
		log.Printf("[tracker] closing session: %v", err)
	}
	t.open = nil
}

var syntheticID = regexp.MustCompile(`^window:\d+$`)

// NormalizeAppID strips GNOME's ".desktop" suffix and folds the synthetic
// per-window IDs GNOME invents for windows without a .desktop file
// ("window:28") into one "unknown" app.
func NormalizeAppID(appID string) string {
	if syntheticID.MatchString(appID) {
		return "unknown"
	}
	return strings.TrimSuffix(appID, ".desktop")
}
