package planner

import (
	"github.com/akkki007/screentime-app/internal/ipc"
)

// OffStatus is planner.status while the planner is disabled: no Planner
// exists, nothing is probed (no PATH search, no model file), and the only
// filesystem access is a stat of the key file, on the user's own request.
func OffStatus(o Options, keyPath string) ipc.PlannerStatus {
	o = normalize(o)
	return ipc.PlannerStatus{
		Provider: o.Provider,
		State:    "off",
		KeySet:   KeySet(keyPath),
		Host:     remoteHost(o),
	}
}

func remoteHost(o Options) string {
	if o.Provider != "remote" {
		return ""
	}
	if u, err := ParseEndpoint(o.Endpoint); err == nil {
		return u.Hostname()
	}
	return ""
}

// Status describes the configured provider. For the local one it looks for
// the server and the model file, which is cheap and happens only when the UI
// asks. State "off" for a local planner means the sidecar is not running
// right now; it starts on the next request.
func (p *Planner) Status() ipc.PlannerStatus {
	o := p.options()
	st := ipc.PlannerStatus{
		Enabled:  true,
		Provider: o.Provider,
		State:    "off",
		KeySet:   KeySet(p.cfg.KeyPath),
		Host:     remoteHost(o),
	}
	p.mu.Lock()
	lastErr := p.lastErr
	sup := p.sup
	p.mu.Unlock()

	var problem string
	if o.Provider == "remote" {
		switch _, err := ParseEndpoint(o.Endpoint); {
		case err != nil:
			problem = err.Error()
		case o.Model == "":
			problem = "no model name set for the remote provider"
		default:
			st.State = "ready" // configured
		}
	} else {
		_, serverErr := FindServer(o.ServerPath)
		_, modelErr := FindModel(o.ModelPath)
		st.ServerFound, st.ModelFound = serverErr == nil, modelErr == nil
		switch {
		case serverErr != nil:
			problem = serverErr.Error()
		case modelErr != nil:
			problem = modelErr.Error()
		case sup != nil:
			state, err := sup.Status()
			st.State = string(state)
			if state == StateError {
				problem = err
			}
		}
	}
	if problem == "" && lastErr != "" {
		problem = lastErr
	}
	if problem != "" {
		st.State = "error"
		st.LastError = &problem
	}
	return st
}
