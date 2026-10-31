// Package notch decides what the top-of-screen notch shows (ADR 14). It holds
// no UI: each desktop's notch feeds it the daemon's events and draws the View
// it returns. adapters/gnome-extension/notch.js is a port for the GNOME Shell;
// both pass testdata/fixtures.json, so every desktop says the same thing.
package notch

import (
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/akkki007/screentime-app/internal/ipc"
)

// How long a temporary view stays, in ms.
const (
	AlertMs    = 6_000
	ReminderMs = 6_000
	GreetingMs = 5_000
	// maxDetail caps a reminder's message in the pill.
	maxDetail = 60
	// PanelApps is how many apps the expanded panel lists.
	PanelApps = 4
)

// View is what the pill shows.
type View struct {
	Kind   string `json:"kind"` // offline, idle, paused, focus, alert, reminder, greeting
	Icon   string `json:"icon"` // clock, pause, timer, limit, break, moon, bolt, wave, offline
	Text   string `json:"text"`
	Detail string `json:"detail"`
	// CountdownTo is when focus mode ends (Unix ms), for a ring or bar.
	CountdownTo *int64 `json:"countdownTo"`
}

// App is one app's time today.
type App struct {
	AppID string `json:"appId"`
	Name  string `json:"name"`
	Ms    int64  `json:"ms"`
}

// PanelApp is a row in the expanded panel.
type PanelApp struct {
	Name    string `json:"name"`
	Time    string `json:"time"`
	Percent int    `json:"percent"`
}

// Panel is the expanded view under the pill.
type Panel struct {
	Total  string     `json:"total"`
	Apps   []PanelApp `json:"apps"`
	Paused bool       `json:"paused"`
	Focus  bool       `json:"focus"`
}

// Options are the per-user facts the model can't learn from events.
type Options struct {
	// UserName is the account's real name; its first word greets the user.
	UserName string `json:"userName"`
	// TZOffsetMin is the local offset from UTC in minutes (east positive),
	// for "until 14:05" and the greeting's part of the day.
	TZOffsetMin int `json:"tzOffsetMin"`
}

type temp struct {
	view     View
	priority int // 1 is highest
	until    int64
}

// Model is the notch's state. It is not safe for concurrent use.
type Model struct {
	opts      Options
	connected bool
	status    ipc.TrackerStatus
	apps      []App
	total     int64
	temp      *temp
	greeted   string // local day already greeted, "2006-01-02"
}

// New returns a model that is not yet connected.
func New(o Options) *Model { return &Model{opts: o} }

// SetOptions updates the user's name or time zone.
func (m *Model) SetOptions(o Options) { m.opts = o }

// Connected records whether the daemon is reachable.
func (m *Model) Connected(c bool) {
	m.connected = c
	if !c {
		m.status = ipc.TrackerStatus{}
	}
}

// Status records the tracker's status, and greets the user at the first
// activity of a local day.
func (m *Model) Status(s ipc.TrackerStatus, at int64) {
	m.status = s
	m.connected = true
	active := s.CurrentAppID != nil && *s.CurrentAppID != "" && !s.Idle && !s.Paused
	if day := m.localDay(at); active && day != m.greeted {
		m.greeted = day
		m.show(View{Kind: "greeting", Icon: "wave", Text: m.greeting(at)}, 3, at+GreetingMs, at)
	}
}

// Today records today's per-app totals.
func (m *Model) Today(apps []App) {
	m.apps = append(m.apps[:0], apps...)
	sort.SliceStable(m.apps, func(i, j int) bool { return m.apps[i].Ms > m.apps[j].Ms })
	m.total = 0
	for _, a := range apps {
		m.total += a.Ms
	}
}

// LimitHit shows that target (an app's name or a category) reached its
// daily limit.
func (m *Model) LimitHit(target string, dailyMs int64, at int64) {
	m.show(View{Kind: "alert", Icon: "limit", Text: "Daily limit reached",
		Detail: target + " · " + FormatShort(dailyMs)}, 1, at+AlertMs, at)
}

// Reminder shows one of the daemon's reminders: kind is break, downtime or
// focus.
func (m *Model) Reminder(kind, message string, at int64) {
	v := View{Kind: "reminder", Detail: clip(message, maxDetail)}
	switch kind {
	case "break":
		v.Icon, v.Text = "break", "Time for a break"
	case "downtime":
		v.Icon, v.Text = "moon", "Downtime"
	default:
		v.Icon, v.Text = "bolt", "Stay focused"
	}
	m.show(v, 2, at+ReminderMs, at)
}

func (m *Model) show(v View, priority int, until, at int64) {
	if t := m.temp; t != nil && at < t.until && t.priority < priority {
		return // something more important is showing
	}
	m.temp = &temp{view: v, priority: priority, until: until}
}

// View is what to show at now.
func (m *Model) View(now int64) View {
	if t := m.temp; t != nil {
		if now < t.until {
			return t.view
		}
		m.temp = nil
	}
	s := m.status
	switch {
	case !m.connected:
		return View{Kind: "offline", Icon: "offline", Text: "Screentime is not running"}
	case s.FocusMode.Active && s.FocusMode.Until != nil:
		until := *s.FocusMode.Until
		return View{Kind: "focus", Icon: "timer", Text: Countdown(until - now), Detail: "Focus", CountdownTo: &until}
	case s.Paused:
		v := View{Kind: "paused", Icon: "pause", Text: "Paused"}
		if s.ResumeAt != nil {
			v.Detail = "until " + m.clock(*s.ResumeAt)
		}
		return v
	}
	return View{Kind: "idle", Icon: "clock", Text: FormatShort(m.total)}
}

// NextChange is when the view would change with no new event (a temporary
// view ending, or the focus countdown's next second), or 0 for never. The
// notch sleeps until then: no polling.
func (m *Model) NextChange(now int64) int64 {
	next := int64(0)
	if t := m.temp; t != nil && now < t.until {
		next = t.until
	}
	if s := m.status; m.connected && s.FocusMode.Active && s.FocusMode.Until != nil && now < *s.FocusMode.Until {
		// The countdown rounds up, so it changes when the time left crosses
		// a whole second.
		step := (*s.FocusMode.Until - now) % 1000
		if step == 0 {
			step = 1000
		}
		if tick := now + step; next == 0 || tick < next {
			next = tick
		}
	}
	return next
}

// Panel is the expanded view.
func (m *Model) Panel() Panel {
	p := Panel{Total: FormatShort(m.total), Paused: m.status.Paused, Focus: m.status.FocusMode.Active, Apps: []PanelApp{}}
	for i, a := range m.apps {
		if i == PanelApps {
			break
		}
		pct := 0
		if m.total > 0 {
			pct = int((a.Ms*100 + m.total/2) / m.total)
		}
		name := a.Name
		if name == "" {
			name = a.AppID
		}
		p.Apps = append(p.Apps, PanelApp{Name: name, Time: FormatShort(a.Ms), Percent: pct})
	}
	return p
}

// FormatShort is the dashboard's compact duration: <1m, 45m, 3h, 3h 17m.
func FormatShort(ms int64) string {
	minutes := (ms + 30_000) / 60_000
	if minutes < 1 {
		if ms > 0 {
			return "<1m"
		}
		return "0m"
	}
	h, m := minutes/60, minutes%60
	switch {
	case h == 0:
		return fmt.Sprintf("%dm", m)
	case m == 0:
		return fmt.Sprintf("%dh", h)
	}
	return fmt.Sprintf("%dh %dm", h, m)
}

// Countdown renders the time left as 24:13, or 1:05:00 from an hour.
func Countdown(ms int64) string {
	if ms < 0 {
		ms = 0
	}
	s := (ms + 999) / 1000
	h, m, sec := s/3600, s/60%60, s%60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, sec)
	}
	return fmt.Sprintf("%02d:%02d", m, sec)
}

func (m *Model) local(at int64) time.Time {
	return time.UnixMilli(at).UTC().Add(time.Duration(m.opts.TZOffsetMin) * time.Minute)
}

func (m *Model) localDay(at int64) string { return m.local(at).Format("2006-01-02") }

func (m *Model) clock(at int64) string { return m.local(at).Format("15:04") }

func (m *Model) greeting(at int64) string {
	part := "evening"
	switch h := m.local(at).Hour(); {
	case h < 12:
		part = "morning"
	case h < 18:
		part = "afternoon"
	}
	if first := strings.Fields(m.opts.UserName); len(first) > 0 {
		return "Good " + part + ", " + first[0] + "!"
	}
	return "Good " + part + "!"
}

func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return strings.TrimRight(string(r[:n-1]), " ") + "…"
}
