// Package daemon wires the tracker, web tracker, rules engine and RPC
// handlers together. Port of apps/daemon/src/index.ts.
//
// One mutex serialises every entry point (RPC requests, provider callbacks
// and the heartbeat), so the components can stay single-threaded like the
// Bun daemon they were ported from.
package daemon

import (
	"database/sql"
	"encoding/json"
	"log"
	"sync"
	"time"

	"github.com/akkki007/screentime-app/internal/apps"
	"github.com/akkki007/screentime-app/internal/categories"
	"github.com/akkki007/screentime-app/internal/focus"
	"github.com/akkki007/screentime-app/internal/ipc"
	"github.com/akkki007/screentime-app/internal/notify"
	"github.com/akkki007/screentime-app/internal/queries"
	"github.com/akkki007/screentime-app/internal/rpc"
	"github.com/akkki007/screentime-app/internal/rules"
	"github.com/akkki007/screentime-app/internal/settings"
	"github.com/akkki007/screentime-app/internal/tracker"
)

// Broadcaster sends notifications to connected clients.
type Broadcaster interface {
	Broadcast(method string, params any)
}

type nobody struct{}

func (nobody) Broadcast(string, any) {}

// Config is what the daemon needs from its environment.
type Config struct {
	DB       *sql.DB
	Provider focus.Provider
	Notifier notify.Notifier
	Now      func() int64
	Lookup   apps.Lookup
	Debug    bool
}

// Daemon is the running core.
type Daemon struct {
	mu       sync.Mutex
	cfg      Config
	settings ipc.Settings
	tracker  *tracker.Tracker
	web      *tracker.WebTracker
	rules    *rules.Engine
	hub      Broadcaster
}

// New builds the daemon; call SetBroadcaster once the RPC server exists,
// then Start.
func New(cfg Config) (*Daemon, error) {
	if err := apps.Backfill(cfg.DB, cfg.Lookup); err != nil {
		return nil, err
	}
	s, err := settings.Get(cfg.DB)
	if err != nil {
		return nil, err
	}
	d := &Daemon{cfg: cfg, settings: s, hub: nobody{}}

	var debug func(string)
	if cfg.Debug {
		debug = func(m string) { log.Printf("[tracker] %s", m) }
	}
	d.tracker = tracker.New(cfg.DB, cfg.Provider, tracker.Options{
		IdleThresholdMs: s.IdleThresholdMinutes * 60_000,
		CaptureTitles:   s.CaptureTitles,
		Now:             cfg.Now,
		Lookup:          cfg.Lookup,
		Serialize:       d.locked,
		Debug:           debug,
	})
	d.web = tracker.NewWebTracker(cfg.DB, cfg.Now)
	d.rules = rules.New(rules.Deps{
		DB:           cfg.DB,
		Notifier:     cfg.Notifier,
		Now:          cfg.Now,
		Settings:     func() ipc.Settings { return d.settings },
		State:        d.tracker.State,
		ActiveDomain: d.web.ActiveDomain,
		LimitHit:     func(e ipc.LimitHitEvent) { d.hub.Broadcast("event.limitHit", e) },
		Reminder:     func(e ipc.ReminderEvent) { d.hub.Broadcast("event.reminder", e) },
	})
	d.tracker.OnEvent(d.onTrackerEvent)
	return d, nil
}

func (d *Daemon) locked(f func()) {
	d.mu.Lock()
	defer d.mu.Unlock()
	f()
}

// SetBroadcaster connects notifications to the RPC server.
func (d *Daemon) SetBroadcaster(b Broadcaster) { d.locked(func() { d.hub = b }) }

// Start subscribes to the focus provider. Call it before serving requests.
func (d *Daemon) Start() { d.tracker.Start() }

// Heartbeat extends open sessions and evaluates the rules; call it every
// tracker.HeartbeatMs.
func (d *Daemon) Heartbeat() {
	d.locked(func() {
		d.tracker.Heartbeat()
		d.web.Sync(d.browserInUse())
		d.rules.Tick()
	})
}

// Run calls Heartbeat on a ticker until stop is closed.
func (d *Daemon) Run(stop <-chan struct{}) {
	ticker := time.NewTicker(tracker.HeartbeatMs * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			d.Heartbeat()
		case <-stop:
			return
		}
	}
}

// Stop closes the open sessions.
func (d *Daemon) Stop() {
	d.locked(func() {
		d.tracker.Stop()
		d.web.CloseAt(d.cfg.Now())
	})
}

func (d *Daemon) onTrackerEvent(e tracker.Event) {
	d.rules.OnTrackerEvent(e)
	switch e.Type {
	case tracker.EventIdle:
		d.web.CloseAt(e.At)
	case tracker.EventPaused:
		d.web.CloseAt(d.cfg.Now())
	default:
		d.web.Sync(d.browserInUse())
	}
	if e.Type == tracker.EventFocus {
		d.hub.Broadcast("event.focus", ipc.FocusEvent{AppID: e.AppID, Since: e.At})
	}
	d.hub.Broadcast("event.status", d.status())
}

// browserInUse reports whether a browser is the app being counted.
func (d *Daemon) browserInUse() bool {
	id := d.tracker.State().CurrentAppID
	return id != "" && categories.DefaultFor(id) == "Browsing"
}

func (d *Daemon) status() ipc.TrackerStatus {
	s := d.tracker.State()
	st := ipc.TrackerStatus{
		Paused:    s.Paused,
		ResumeAt:  s.ResumeAt,
		Idle:      s.Idle,
		Since:     s.Since,
		FocusMode: d.rules.FocusMode(),
	}
	if s.CurrentAppID != "" {
		id := s.CurrentAppID
		st.CurrentAppID = &id
	}
	return st
}

func (d *Daemon) configureTracker() {
	d.tracker.Configure(d.settings.CaptureTitles, d.settings.IdleThresholdMinutes*60_000)
}

// Handle implements rpc.Handler.
func (d *Daemon) Handle(method string, params json.RawMessage) (result any, err error) {
	d.locked(func() { result, err = d.handle(method, params) })
	return result, err
}

func (d *Daemon) handle(method string, params json.RawMessage) (any, error) {
	db := d.cfg.DB
	switch method {
	case "usage.summary":
		req, err := ipc.ParseUsageSummary(params)
		if err != nil {
			return nil, err
		}
		return queries.UsageSummary(db, req)
	case "usage.timeline":
		date, err := ipc.ParseUsageTimeline(params)
		if err != nil {
			return nil, err
		}
		return queries.Timeline(db, date)
	case "usage.web":
		from, to, err := ipc.ParseRange(params)
		if err != nil {
			return nil, err
		}
		return queries.UsageWeb(db, from, to)

	case "limits.list":
		if err := ipc.ExpectVoid(params); err != nil {
			return nil, err
		}
		return queries.ListLimits(db)
	case "limits.set":
		l, err := ipc.ParseLimit(params)
		if err != nil {
			return nil, err
		}
		return queries.SetLimit(db, l)
	case "limits.delete":
		id, err := ipc.ParseID(params)
		if err != nil {
			return nil, err
		}
		ok, err := queries.DeleteLimit(db, id)
		return ipc.OK{OK: ok}, err

	case "apps.list":
		if err := ipc.ExpectVoid(params); err != nil {
			return nil, err
		}
		return queries.ListApps(db)
	case "apps.setCategory":
		appID, categoryID, err := ipc.ParseSetAppCategory(params)
		if err != nil {
			return nil, err
		}
		ok, err := queries.SetAppCategory(db, appID, categoryID)
		return ipc.OK{OK: ok}, err
	case "categories.list":
		if err := ipc.ExpectVoid(params); err != nil {
			return nil, err
		}
		return queries.ListCategories(db)

	case "settings.get":
		if err := ipc.ExpectVoid(params); err != nil {
			return nil, err
		}
		return d.settings, nil
	case "settings.set":
		patch, err := ipc.ParseSettingsPatch(params)
		if err != nil {
			return nil, err
		}
		s, err := settings.Patch(db, patch)
		if err != nil {
			return nil, err
		}
		d.settings = s
		d.configureTracker()
		return s, nil

	case "tracker.pause":
		minutes, err := ipc.ParseMinutes(params)
		if err != nil {
			return nil, err
		}
		return map[string]int64{"resumeAt": d.tracker.Pause(minutes)}, nil
	case "tracker.resume":
		if err := ipc.ExpectVoid(params); err != nil {
			return nil, err
		}
		d.tracker.Resume()
		return ipc.OK{OK: true}, nil
	case "tracker.status":
		if err := ipc.ExpectVoid(params); err != nil {
			return nil, err
		}
		return d.status(), nil

	case "focus.start":
		minutes, err := ipc.ParseMinutes(params)
		if err != nil {
			return nil, err
		}
		until := d.rules.StartFocusMode(minutes)
		d.hub.Broadcast("event.status", d.status())
		return map[string]int64{"until": until}, nil
	case "focus.stop":
		if err := ipc.ExpectVoid(params); err != nil {
			return nil, err
		}
		d.rules.StopFocusMode()
		d.hub.Broadcast("event.status", d.status())
		return ipc.OK{OK: true}, nil

	case "data.export":
		req, err := ipc.ParseDataExport(params)
		if err != nil {
			return nil, err
		}
		return queries.ExportData(db, req.Format, req.From, req.To, d.cfg.Now())
	case "data.wipe":
		everything, err := ipc.ParseDataWipe(params)
		if err != nil {
			return nil, err
		}
		if err := queries.WipeData(db, everything); err != nil {
			return nil, err
		}
		// The open sessions pointed at rows that no longer exist.
		d.tracker.RestartSession()
		d.web.Reset()
		if everything {
			s, err := settings.Get(db)
			if err != nil {
				return nil, err
			}
			d.settings = s
			d.configureTracker()
		}
		return ipc.OK{OK: true}, nil

	case "browser.activeTab":
		tab, err := ipc.ParseBrowserActiveTab(params)
		if err != nil {
			log.Printf("[rpc] ignoring malformed browser.activeTab: %v", err)
			return nil, nil
		}
		if d.cfg.Debug {
			domain := "<none>"
			if tab.Domain != nil {
				domain = *tab.Domain
			}
			log.Printf("[daemon] browser.activeTab -> domain=%s active=%t", domain, tab.Active)
		}
		d.web.Handle(tab.Domain, tab.Active, d.browserInUse())
		return nil, nil
	}
	return nil, rpc.ErrMethodNotFound
}
