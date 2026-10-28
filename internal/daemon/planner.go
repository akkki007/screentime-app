package daemon

import (
	"encoding/json"
	"strings"

	"github.com/akkki007/screentime-app/internal/ipc"
	"github.com/akkki007/screentime-app/internal/planner"
	"github.com/akkki007/screentime-app/internal/rpc"
)

// The optional planner (ADR 8) is wired in here and nowhere else. While
// plannerEnabled is false d.planner is nil and none of this runs.

func isPlannerMethod(method string) bool { return strings.HasPrefix(method, "planner.") }

// IsAsync implements rpc.AsyncHandler: model calls take seconds to minutes,
// so they are answered from their own goroutine.
func (d *Daemon) IsAsync(method string) bool {
	return method == "planner.suggest" || method == "planner.categorize"
}

func plannerOptions(s ipc.Settings) planner.Options {
	return planner.Options{
		Provider:    s.PlannerProvider,
		Endpoint:    s.PlannerEndpoint,
		Model:       s.PlannerModel,
		ServerPath:  s.PlannerServerPath,
		ModelPath:   s.PlannerModelPath,
		IdleMinutes: s.PlannerIdleMinutes,
	}
}

// syncPlanner makes d.planner match the settings: created when enabled (it
// still does nothing until asked), reconfigured when its settings change,
// stopped and dropped when disabled. The caller holds d.mu, or is New.
func (d *Daemon) syncPlanner() {
	switch {
	case d.settings.PlannerEnabled && d.planner == nil:
		d.planner = planner.New(planner.Config{
			DB:      d.cfg.DB,
			Now:     d.cfg.Now,
			Lookup:  d.cfg.Lookup,
			KeyPath: d.cfg.PlannerKeyPath,
			RunDir:  d.cfg.PlannerRunDir,
			Options: plannerOptions(d.settings),
		})
	case d.settings.PlannerEnabled:
		d.planner.Configure(plannerOptions(d.settings))
	case d.planner != nil:
		// Stopping waits for the process to exit, which can take seconds:
		// not while holding the lock that ticks need.
		p := d.planner
		d.planner = nil
		d.closing.Add(1)
		go func() {
			defer d.closing.Done()
			p.Close()
		}()
	}
}

// closePlanner stops the planner and its sidecar for good (daemon shutdown).
func (d *Daemon) closePlanner() {
	var p *planner.Planner
	d.locked(func() { p, d.planner = d.planner, nil })
	if p != nil {
		p.Close()
	}
	d.closing.Wait()
}

func (d *Daemon) plannerState() (p *planner.Planner, s ipc.Settings) {
	d.locked(func() { p, s = d.planner, d.settings })
	return p, s
}

// handlePlanner serves planner.* without holding d.mu. Parameters are
// validated first, so a malformed request gets the same answer whether or
// not the planner is on.
func (d *Daemon) handlePlanner(method string, params json.RawMessage) (any, error) {
	switch method {
	case "planner.status":
		if err := ipc.ExpectVoid(params); err != nil {
			return nil, err
		}
		p, s := d.plannerState()
		if p == nil {
			return planner.OffStatus(plannerOptions(s), d.cfg.PlannerKeyPath), nil
		}
		return p.Status(), nil

	case "planner.setKey":
		// Works while the planner is off, so a key can be entered before the
		// feature is switched on.
		key, err := ipc.ParsePlannerSetKey(params)
		if err != nil {
			return nil, err
		}
		if err := planner.WriteKey(d.cfg.PlannerKeyPath, key); err != nil {
			return nil, err
		}
		if p, _ := d.plannerState(); p != nil {
			p.KeyChanged()
		}
		return ipc.OK{OK: true}, nil

	case "planner.stop":
		if err := ipc.ExpectVoid(params); err != nil {
			return nil, err
		}
		if p, _ := d.plannerState(); p != nil {
			p.Stop()
		}
		return ipc.OK{OK: true}, nil

	case "planner.suggest":
		goal, err := ipc.ParsePlannerSuggest(params)
		if err != nil {
			return nil, err
		}
		p, _ := d.plannerState()
		if p == nil {
			return nil, planner.ErrOff
		}
		return p.Suggest(goal)

	case "planner.categorize":
		appIDs, err := ipc.ParsePlannerCategorize(params)
		if err != nil {
			return nil, err
		}
		p, _ := d.plannerState()
		if p == nil {
			return nil, planner.ErrOff
		}
		return p.Categorize(appIDs)
	}
	return nil, rpc.ErrMethodNotFound
}
