package notch

import (
	"encoding/json"
	"os/user"
	"sync"
	"time"

	"github.com/akkki007/screentime-app/internal/ipc"
	"github.com/akkki007/screentime-app/internal/shell"
)

// Driver connects a native notch to the daemon (ADR 14). It keeps a Model up
// to date from the daemon's events and calls OnChange whenever the pill or
// the panel may look different. It never polls for state: it sleeps until an
// event arrives, a temporary view ends, the focus countdown ticks, or (for
// today's total) a minute has passed.
type Driver struct {
	// OnChange is called, off the caller's goroutine, when the view may have
	// changed. The notch then reads View and Panel.
	OnChange func()
	// Now is the clock; tests replace it.
	Now func() time.Time

	conn *shell.Conn

	mu     sync.Mutex
	model  *Model
	limits map[int64]ipc.Limit
	names  map[string]string
	redraw *time.Timer
	minute *time.Timer
}

// todayEvery refreshes today's total while connected; the pill shows minutes.
const todayEvery = time.Minute

// NewDriver returns a driver for the daemon at the default socket. Pass
// conn options only in tests.
func NewDriver(onChange func(), opts ...shell.Options) *Driver {
	d := &Driver{OnChange: onChange, Now: time.Now, names: map[string]string{}, limits: map[int64]ipc.Limit{}}
	d.model = New(Options{UserName: realName()})
	o := shell.Options{}
	if len(opts) > 0 {
		o = opts[0]
	}
	o.OnState = d.onState
	o.OnNotification = d.onNotification
	d.conn = shell.NewConn(o)
	return d
}

// Start connects and keeps reconnecting.
func (d *Driver) Start() { d.conn.Start() }

// Stop disconnects and cancels the timers.
func (d *Driver) Stop() {
	d.conn.Stop()
	d.mu.Lock()
	for _, t := range []*time.Timer{d.redraw, d.minute} {
		if t != nil {
			t.Stop()
		}
	}
	d.mu.Unlock()
}

// View is what the pill shows now.
func (d *Driver) View() View {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.model.View(d.now())
}

// Panel is the expanded view.
func (d *Driver) Panel() Panel {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.model.Panel()
}

// TogglePause pauses tracking for 15 minutes, or resumes it.
func (d *Driver) TogglePause() error {
	if d.Panel().Paused {
		_, err := d.conn.Call("tracker.resume", nil)
		return err
	}
	_, err := d.conn.Call("tracker.pause", map[string]int{"minutes": 15})
	return err
}

// ToggleFocus starts a 25 minute focus session, or ends the current one.
func (d *Driver) ToggleFocus() error {
	if d.Panel().Focus {
		_, err := d.conn.Call("focus.stop", nil)
		return err
	}
	_, err := d.conn.Call("focus.start", map[string]int{"minutes": 25})
	return err
}

func (d *Driver) now() int64 { return d.Now().UnixMilli() }

func (d *Driver) onState(s shell.State) {
	d.mu.Lock()
	d.model.Connected(s.Connected)
	d.mu.Unlock()
	if s.Connected {
		go d.resync()
	} else {
		d.mu.Lock()
		if d.minute != nil {
			d.minute.Stop()
		}
		d.mu.Unlock()
	}
	d.changed()
}

// resync loads everything a fresh connection needs.
func (d *Driver) resync() {
	if raw, err := d.conn.Call("tracker.status", nil); err == nil {
		var st ipc.TrackerStatus
		if json.Unmarshal(raw, &st) == nil {
			d.mu.Lock()
			d.model.SetOptions(d.options())
			d.model.Status(st, d.now())
			d.mu.Unlock()
		}
	}
	d.loadNames()
	d.loadLimits()
	d.refreshToday()
}

func (d *Driver) onNotification(method string, params json.RawMessage) {
	switch method {
	case "event.status":
		var st ipc.TrackerStatus
		if json.Unmarshal(params, &st) != nil {
			return
		}
		d.mu.Lock()
		d.model.SetOptions(d.options())
		d.model.Status(st, d.now())
		d.mu.Unlock()
		go d.refreshToday()
	case "event.focus":
		go d.refreshToday()
	case "event.limitHit":
		var e ipc.LimitHitEvent
		if json.Unmarshal(params, &e) != nil {
			return
		}
		go d.limitHit(e.LimitID)
		return
	case "event.reminder":
		var e ipc.ReminderEvent
		if json.Unmarshal(params, &e) != nil {
			return
		}
		d.mu.Lock()
		d.model.Reminder(e.Kind, e.Message, d.now())
		d.mu.Unlock()
	default:
		return
	}
	d.changed()
}

func (d *Driver) limitHit(id int64) {
	d.mu.Lock()
	l, ok := d.limits[id]
	d.mu.Unlock()
	if !ok { // a limit added since we last looked
		d.loadLimits()
		d.mu.Lock()
		l, ok = d.limits[id]
		d.mu.Unlock()
		if !ok {
			return
		}
	}
	d.mu.Lock()
	target := l.Target
	if l.TargetType == "app" {
		if n := d.names[l.Target]; n != "" {
			target = n
		}
	}
	d.model.LimitHit(target, l.DailyMs, d.now())
	d.mu.Unlock()
	d.changed()
}

func (d *Driver) loadLimits() {
	raw, err := d.conn.Call("limits.list", nil)
	if err != nil {
		return
	}
	var ls []ipc.Limit
	if json.Unmarshal(raw, &ls) != nil {
		return
	}
	d.mu.Lock()
	d.limits = map[int64]ipc.Limit{}
	for _, l := range ls {
		if l.ID != nil {
			d.limits[*l.ID] = l
		}
	}
	d.mu.Unlock()
}

func (d *Driver) loadNames() {
	raw, err := d.conn.Call("apps.list", nil)
	if err != nil {
		return
	}
	var apps []struct {
		AppID string  `json:"appId"`
		Name  *string `json:"name"`
	}
	if json.Unmarshal(raw, &apps) != nil {
		return
	}
	d.mu.Lock()
	for _, a := range apps {
		if a.Name != nil && *a.Name != "" {
			d.names[a.AppID] = *a.Name
		}
	}
	d.mu.Unlock()
}

// refreshToday reloads today's per-app totals, and schedules the next
// refresh a minute out.
func (d *Driver) refreshToday() {
	now := d.Now()
	y, mo, day := now.Date()
	midnight := time.Date(y, mo, day, 0, 0, 0, 0, now.Location())
	raw, err := d.conn.Call("usage.summary", map[string]any{
		"from": midnight.UnixMilli(), "to": now.UnixMilli() + 60_000, "groupBy": "app",
	})
	if err != nil {
		return
	}
	var rows []struct {
		Key string `json:"key"`
		Ms  int64  `json:"ms"`
	}
	if json.Unmarshal(raw, &rows) != nil {
		return
	}
	d.mu.Lock()
	unknown := false
	apps := make([]App, 0, len(rows))
	for _, r := range rows {
		name := d.names[r.Key]
		unknown = unknown || name == ""
		apps = append(apps, App{AppID: r.Key, Name: name, Ms: r.Ms})
	}
	d.model.Today(apps)
	if d.minute != nil {
		d.minute.Stop()
	}
	d.minute = time.AfterFunc(todayEvery, d.refreshToday)
	d.mu.Unlock()
	if unknown {
		d.loadNames() // a new app; the next refresh names it
	}
	d.changed()
}

// changed tells the notch to redraw, and arms one timer for the next change
// the model knows about without an event.
func (d *Driver) changed() {
	d.mu.Lock()
	next := d.model.NextChange(d.now())
	if d.redraw != nil {
		d.redraw.Stop()
		d.redraw = nil
	}
	if next > 0 {
		wait := time.Duration(next-d.now()) * time.Millisecond
		d.redraw = time.AfterFunc(wait, d.changed)
	}
	d.mu.Unlock()
	if d.OnChange != nil {
		d.OnChange()
	}
}

func (d *Driver) options() Options {
	_, off := d.Now().Zone()
	o := d.model.opts
	o.TZOffsetMin = off / 60
	return o
}

// realName is the account's full name, or "" (the greeting then has none).
func realName() string {
	u, err := user.Current()
	if err != nil {
		return ""
	}
	return u.Name
}
