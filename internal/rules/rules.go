// Package rules is the wellbeing engine: daily limits, break reminders,
// downtime and focus mode. Port of apps/daemon/src/rules.ts.
//
// Everything is derived from the database and the clock on each Tick rather
// than from timers, so a suspend can't make a limit miss or double-fire:
// after wake the next tick simply sees the new totals (ADR 4). Limits are
// nudges (a notification and a UI overlay), not locks.
//
// Not safe for concurrent use; the daemon serialises calls.
package rules

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"math"
	"regexp"

	"github.com/akkki007/screentime-app/internal/ipc"
	"github.com/akkki007/screentime-app/internal/notify"
	"github.com/akkki007/screentime-app/internal/queries"
	"github.com/akkki007/screentime-app/internal/timeutil"
	"github.com/akkki007/screentime-app/internal/tracker"
)

const (
	// Overlay and block limits re-appear this often while the target is in use.
	limitRepeatMs      = 5 * 60_000
	breakRepeatMs      = 10 * 60_000
	downtimeRepeatMs   = 15 * 60_000
	focusNudgeRepeatMs = 60_000
	// A gap this long between ticks means the machine was suspended.
	suspendGapMs = 60_000
)

// Deps is what the engine reads and where it reports.
type Deps struct {
	DB       *sql.DB
	Notifier notify.Notifier
	Now      func() int64
	Settings func() ipc.Settings
	State    func() tracker.State
	// ActiveDomain is the browser tab being counted, or "".
	ActiveDomain func() string
	LimitHit     func(ipc.LimitHitEvent)
	Reminder     func(ipc.ReminderEvent)
}

type progress struct {
	day      string
	lastEmit int64
}

// Engine evaluates the rules.
type Engine struct {
	d        Deps
	progress map[int64]progress

	activeSince       int64
	awaySince         *int64
	lastBreakReminder int64
	lastTick          int64

	lastDowntimeNotice int64

	focusUntil     *int64
	lastFocusNudge int64
}

func New(d Deps) *Engine {
	now := d.Now()
	return &Engine{d: d, progress: map[int64]progress{}, activeSince: now, lastTick: now}
}

// FocusMode is the focus-mode part of tracker.status.
func (e *Engine) FocusMode() ipc.FocusMode {
	return ipc.FocusMode{Active: e.focusUntil != nil, Until: e.focusUntil}
}

// StartFocusMode turns focus mode on for the given minutes; returns the end.
func (e *Engine) StartFocusMode(minutes int64) int64 {
	until := e.d.Now() + minutes*60_000
	e.focusUntil = &until
	e.lastFocusNudge = 0
	return until
}

func (e *Engine) StopFocusMode() { e.focusUntil = nil }

// OnTrackerEvent feeds tracker events, so break accounting knows when the
// user was away and focus mode sees app switches.
func (e *Engine) OnTrackerEvent(ev tracker.Event) {
	switch ev.Type {
	case tracker.EventIdle:
		e.leave(ev.At)
	case tracker.EventPaused:
		e.leave(e.d.Now())
	case tracker.EventActive:
		e.returnFromAway(ev.At)
	case tracker.EventResumed:
		e.returnFromAway(e.d.Now())
	case tracker.EventFocus:
		e.checkFocusMode(ev.AppID)
	}
}

// Tick evaluates every rule; the daemon calls it on its 5 s heartbeat.
func (e *Engine) Tick() {
	now := e.d.Now()
	settings := e.d.Settings()
	// Suspend shows up as a long gap between ticks: count it as time away.
	if now-e.lastTick > suspendGapMs {
		e.leave(e.lastTick)
		e.returnFromAway(now)
	}
	e.lastTick = now

	if err := e.checkLimits(now); err != nil {
		log.Printf("[rules] limits: %v", err)
	}
	e.checkBreak(now, settings)
	e.checkDowntime(now, settings)
	e.checkFocusExpiry(now)
}

// --- limits -----------------------------------------------------------------

func (e *Engine) checkLimits(now int64) error {
	from, to := timeutil.StartOfLocalDay(now), timeutil.AddLocalDays(now, 1)
	day := timeutil.LocalDateKey(now)
	limits, err := queries.ListLimits(e.d.DB)
	if err != nil {
		return err
	}
	for _, l := range limits {
		if l.ID == nil || !withinSchedule(l.Schedule, now) {
			continue
		}
		used, err := queries.UsageFor(e.d.DB, l.TargetType, l.Target, from, to)
		if err != nil {
			return err
		}
		if used < l.DailyMs {
			continue
		}
		seen, ok := e.progress[*l.ID]
		first := !ok || seen.day != day
		repeat := ok && !first && l.Action != "notify" && e.isInUse(l) && now-seen.lastEmit >= limitRepeatMs
		if !first && !repeat {
			continue
		}
		e.progress[*l.ID] = progress{day: day, lastEmit: now}
		e.d.LimitHit(ipc.LimitHitEvent{LimitID: *l.ID, Action: l.Action})
		if first {
			e.d.Notifier.Notify("Daily limit reached",
				fmt.Sprintf("%s: you've used %s today.", e.displayName(l), FormatDuration(l.DailyMs)))
		}
	}
	return nil
}

func (e *Engine) isInUse(l ipc.Limit) bool {
	if l.TargetType == "domain" {
		return e.d.ActiveDomain() == l.Target
	}
	current := e.d.State().CurrentAppID
	if current == "" {
		return false
	}
	if l.TargetType == "app" {
		return current == l.Target
	}
	name, _, ok := e.categoryOf(current)
	return ok && name == l.Target
}

// --- break reminders --------------------------------------------------------

func (e *Engine) leave(at int64) {
	if e.awaySince == nil {
		e.awaySince = &at
	}
}

func (e *Engine) returnFromAway(at int64) {
	if e.awaySince == nil {
		return
	}
	awayMs := at - *e.awaySince
	e.awaySince = nil
	if awayMs >= e.d.Settings().BreakLengthMinutes*60_000 {
		e.activeSince = at
		e.lastBreakReminder = 0
	}
}

func (e *Engine) checkBreak(now int64, s ipc.Settings) {
	if !s.BreakRemindersEnabled || e.awaySince != nil {
		return
	}
	if st := e.d.State(); st.Idle || st.Paused {
		return
	}
	activeMs := now - e.activeSince
	if activeMs < s.BreakEveryMinutes*60_000 || now-e.lastBreakReminder < breakRepeatMs {
		return
	}
	e.lastBreakReminder = now
	msg := fmt.Sprintf("You've been active for %s. Take a %d minute break.", FormatDuration(activeMs), s.BreakLengthMinutes)
	e.d.Reminder(ipc.ReminderEvent{Kind: "break", Message: msg})
	e.d.Notifier.Notify("Time for a break", msg)
}

// --- downtime ---------------------------------------------------------------

func (e *Engine) checkDowntime(now int64, s ipc.Settings) {
	if !s.DowntimeEnabled {
		return
	}
	if st := e.d.State(); st.Idle || st.Paused || st.CurrentAppID == "" {
		return
	}
	minute := timeutil.MinutesIntoDay(now)
	if !timeutil.InDailyWindow(minute, timeutil.ParseClock(s.DowntimeStart), timeutil.ParseClock(s.DowntimeEnd)) {
		return
	}
	if now-e.lastDowntimeNotice < downtimeRepeatMs {
		return
	}
	e.lastDowntimeNotice = now
	msg := fmt.Sprintf("Downtime is on until %s. Time to wind down.", s.DowntimeEnd)
	e.d.Reminder(ipc.ReminderEvent{Kind: "downtime", Message: msg})
	e.d.Notifier.Notify("Downtime", msg)
}

// --- focus mode -------------------------------------------------------------

func (e *Engine) checkFocusMode(appID string) {
	if e.focusUntil == nil {
		return
	}
	now := e.d.Now()
	if now-e.lastFocusNudge < focusNudgeRepeatMs {
		return
	}
	// Distracting means a category explicitly marked unproductive (0).
	_, productive, ok := e.categoryOf(appID)
	if !ok || productive == nil || *productive != 0 {
		return
	}
	e.lastFocusNudge = now
	msg := e.appName(appID) + " is distracting. Focus mode is on."
	e.d.Reminder(ipc.ReminderEvent{Kind: "focus", Message: msg})
	e.d.Notifier.Notify("Stay focused", msg)
}

func (e *Engine) checkFocusExpiry(now int64) {
	if e.focusUntil == nil || now < *e.focusUntil {
		return
	}
	e.focusUntil = nil
	msg := "Focus session finished. Nice work."
	e.d.Reminder(ipc.ReminderEvent{Kind: "focus", Message: msg})
	e.d.Notifier.Notify("Focus mode ended", msg)
}

// --- lookups ----------------------------------------------------------------

func (e *Engine) categoryOf(appID string) (name string, productive *int64, ok bool) {
	var p sql.NullInt64
	err := e.d.DB.QueryRow(`SELECT categories.name, categories.productive FROM apps
		JOIN categories ON categories.id = apps.category_id WHERE apps.app_id = ?`, appID).Scan(&name, &p)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			log.Printf("[rules] category of %s: %v", appID, err)
		}
		return "", nil, false
	}
	if p.Valid {
		productive = &p.Int64
	}
	return name, productive, true
}

func (e *Engine) appName(appID string) string {
	var name sql.NullString
	if err := e.d.DB.QueryRow("SELECT name FROM apps WHERE app_id = ?", appID).Scan(&name); err != nil || !name.Valid {
		return appID
	}
	return name.String
}

func (e *Engine) displayName(l ipc.Limit) string {
	if l.TargetType == "app" {
		return e.appName(l.Target)
	}
	return l.Target
}

var scheduleRe = regexp.MustCompile(`^(\d{2}:\d{2})-(\d{2}:\d{2})$`)

func withinSchedule(schedule string, now int64) bool {
	m := scheduleRe.FindStringSubmatch(schedule)
	if m == nil {
		return true
	}
	return timeutil.InDailyWindow(timeutil.MinutesIntoDay(now), timeutil.ParseClock(m[1]), timeutil.ParseClock(m[2]))
}

// FormatDuration renders "25 min", "2 h" or "1 h 5 min".
func FormatDuration(ms int64) string {
	total := int64(math.Round(float64(ms) / 60_000))
	h, m := total/60, total%60
	switch {
	case h == 0:
		return fmt.Sprintf("%d min", m)
	case m == 0:
		return fmt.Sprintf("%d h", h)
	default:
		return fmt.Sprintf("%d h %d min", h, m)
	}
}
