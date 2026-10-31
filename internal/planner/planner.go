// Package planner is the optional daily planner and AI categorisation
// (docs/adr/0008-local-planner-sidecar.md). It asks a language model for
// suggestions and for categories of apps the built-in rules left in Other.
//
// The package costs nothing until it is used. The daemon holds a nil
// *Planner while the feature is off, and even a Planner starts no goroutine,
// no process and no connection until a request arrives; connections are never
// kept alive.
//
// Two providers, chosen in settings:
//
//   - "local": a llama-server child process (Supervisor), started on demand
//     and stopped after an idle period.
//   - "remote": any OpenAI-compatible /v1/chat/completions endpoint with the
//     user's own API key.
//
// Either way the model only ever sees aggregates (see data.go) and its answer
// is validated against what really exists before anything is returned
// (validate.go). It proposes; it never changes limits or settings itself.
package planner

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/akkki007/screentime-app/internal/apps"
	"github.com/akkki007/screentime-app/internal/ipc"
)

// Errors a caller can compare with errors.Is.
var (
	ErrOff  = errors.New("planner is off")
	ErrBusy = errors.New("the planner is busy with other requests; try again shortly")
)

// maxAdmitted is how many requests may be running or waiting: one at a time
// (a small model on a CPU cannot do better), and a short queue behind it.
const maxAdmitted = 3

// Options is the part of the settings the planner reads.
type Options struct {
	Provider    string // "local" | "remote"
	Endpoint    string // remote base URL, e.g. https://api.openai.com/v1
	Model       string // remote model name
	ServerPath  string // local llama-server; empty means look in PATH
	ModelPath   string // local .gguf file
	IdleMinutes int64  // local sidecar idle shutdown
}

// Config is what a Planner needs from its environment.
type Config struct {
	DB      *sql.DB
	Now     func() int64
	Lookup  apps.Lookup
	KeyPath string // remote API key file; see key.go
	RunDir  string // private runtime directory for the sidecar's key and pid file
	// FetchPath is the screentime-fetch helper for https endpoints; "" looks
	// next to the running binary, then on PATH.
	FetchPath string
	Options   Options
}

// timeouts are variables so tests can shorten them.
type timeouts struct {
	dial     time.Duration // TCP and TLS setup
	response time.Duration // waiting for the answer's headers
	request  time.Duration // one whole request, queue time excluded
	start    time.Duration // loading the local model
	grace    time.Duration // SIGTERM to SIGKILL
	idle     time.Duration // 0: use Options.IdleMinutes
}

func defaultTimeouts() timeouts {
	return timeouts{
		dial:     30 * time.Second,
		response: 120 * time.Second,
		request:  120 * time.Second,
		start:    60 * time.Second,
		grace:    5 * time.Second,
	}
}

// Planner serves planner.suggest and planner.categorize.
type Planner struct {
	cfg Config
	tm  timeouts

	ctx    context.Context
	cancel context.CancelFunc

	slots    chan struct{} // one running request
	admitted atomic.Int32  // running plus waiting

	mu      sync.Mutex
	opts    Options
	sup     *Supervisor // created by the first local request
	lastErr string
}

// New returns a planner. It does no I/O.
func New(cfg Config) *Planner {
	if cfg.Lookup == nil {
		cfg.Lookup = func(string) (apps.Entry, bool) { return apps.Entry{}, false }
	}
	if cfg.Now == nil {
		cfg.Now = func() int64 { return time.Now().UnixMilli() }
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Planner{
		cfg:    cfg,
		tm:     defaultTimeouts(),
		ctx:    ctx,
		cancel: cancel,
		slots:  make(chan struct{}, 1),
		opts:   normalize(cfg.Options),
	}
}

func normalize(o Options) Options {
	if o.Provider != "remote" {
		o.Provider = "local"
	}
	if o.IdleMinutes < 1 {
		o.IdleMinutes = 5
	}
	return o
}

// Configure applies changed settings. The local sidecar is stopped when what
// it was started with no longer applies; a later request starts it afresh.
func (p *Planner) Configure(o Options) {
	o = normalize(o)
	p.mu.Lock()
	old := p.opts
	p.opts = o
	sup := p.sup
	if old != o {
		p.lastErr = ""
	}
	if old.ServerPath != o.ServerPath || old.ModelPath != o.ModelPath || old.IdleMinutes != o.IdleMinutes || o.Provider != "local" {
		p.sup = nil
	} else {
		sup = nil
	}
	p.mu.Unlock()
	if sup != nil {
		go sup.Close()
	}
}

// KeyChanged forgets a remote error that a new key may have fixed.
func (p *Planner) KeyChanged() {
	p.mu.Lock()
	p.lastErr = ""
	p.mu.Unlock()
}

// Stop stops the local sidecar now, if it runs. The next local request
// starts it again.
func (p *Planner) Stop() {
	p.mu.Lock()
	sup := p.sup
	p.mu.Unlock()
	if sup != nil {
		sup.Stop()
	}
}

// Close cancels waiting and running requests and stops the sidecar, waiting
// for the process to exit. The planner is unusable afterwards.
func (p *Planner) Close() {
	p.cancel()
	p.mu.Lock()
	sup := p.sup
	p.sup = nil
	p.mu.Unlock()
	if sup != nil {
		sup.Close()
	}
}

func (p *Planner) options() Options {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.opts
}

func (p *Planner) setErr(err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if err == nil {
		p.lastErr = ""
		return
	}
	p.lastErr = p.describe(err)
}

// describe renders an error for lastError and for the caller: the API key is
// removed from it as a last line of defence, and it is kept short.
func (p *Planner) describe(err error) string {
	msg := err.Error()
	if key := ReadKey(p.cfg.KeyPath); key != "" {
		msg = strings.ReplaceAll(msg, key, "[key]")
	}
	return trimTo(msg, 300)
}

// admit reserves a place in the queue and waits for the running request to
// finish. The returned func releases it.
func (p *Planner) admit() (func(), error) {
	if p.ctx.Err() != nil {
		return nil, ErrOff
	}
	if p.admitted.Add(1) > maxAdmitted {
		p.admitted.Add(-1)
		return nil, ErrBusy
	}
	select {
	case p.slots <- struct{}{}:
		return func() {
			<-p.slots
			p.admitted.Add(-1)
		}, nil
	case <-p.ctx.Done():
		p.admitted.Add(-1)
		return nil, ErrOff
	}
}

// fail records err for planner.status and hands back a description of it
// that is safe to send to the UI.
func (p *Planner) fail(err error) error {
	if p.ctx.Err() != nil || errors.Is(err, ErrOff) {
		return ErrOff // switched off or stopped on purpose: not a failure to report
	}
	p.setErr(err)
	return errors.New(p.describe(err))
}

// Suggest asks the model for a plan for today.
func (p *Planner) Suggest(goal string) (ipc.PlannerSuggestResult, error) {
	release, err := p.admit()
	if err != nil {
		return ipc.PlannerSuggestResult{}, err
	}
	defer release()
	ctx, cancel := context.WithTimeout(p.ctx, p.tm.request)
	defer cancel()

	agg, err := p.gather(goal)
	if err != nil {
		return ipc.PlannerSuggestResult{}, p.fail(err)
	}
	content, err := p.complete(ctx, suggestRequest(agg))
	if err != nil {
		return ipc.PlannerSuggestResult{}, p.fail(err)
	}
	res, err := validateSuggest(content, agg)
	if err != nil {
		return ipc.PlannerSuggestResult{}, p.fail(err)
	}
	p.setErr(nil)
	return res, nil
}

// Categorize asks the model about apps that the rules and the .desktop
// Categories left in Other, and applies what it answers with enough
// confidence. Apps the user categorised are never touched.
func (p *Planner) Categorize(appIDs []string) ([]ipc.PlannerCategorization, error) {
	release, err := p.admit()
	if err != nil {
		return nil, err
	}
	defer release()
	ctx, cancel := context.WithTimeout(p.ctx, p.tm.request)
	defer cancel()

	in, err := p.candidates(appIDs)
	if err != nil {
		return nil, p.fail(err)
	}
	if len(in.apps) == 0 {
		return []ipc.PlannerCategorization{}, nil // nothing to ask, so nothing started
	}
	content, err := p.complete(ctx, categorizeRequest(in))
	if err != nil {
		return nil, p.fail(err)
	}
	answers, err := validateCategorize(content, in)
	if err != nil {
		return nil, p.fail(err)
	}
	applied, err := p.apply(answers)
	if err != nil {
		return nil, p.fail(err)
	}
	p.setErr(nil)
	return applied, nil
}

func logf(format string, args ...any) { log.Printf("[planner] "+format, args...) }

// fetchPath finds the screentime-fetch helper, or "" when there is none.
func (p *Planner) fetchPath() string {
	if p.cfg.FetchPath != "" {
		return p.cfg.FetchPath
	}
	if exe, err := os.Executable(); err == nil {
		helper := filepath.Join(filepath.Dir(exe), "screentime-fetch")
		if st, err := os.Stat(helper); err == nil && st.Mode().IsRegular() {
			return helper
		}
	}
	if path, err := exec.LookPath("screentime-fetch"); err == nil {
		return path
	}
	return ""
}
