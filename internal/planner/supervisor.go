package planner

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"
)

// State is the sidecar's lifecycle state, as planner.status reports it.
type State string

const (
	StateOff      State = "off" // not running: the next request starts it
	StateStarting State = "starting"
	StateReady    State = "ready"
	StateError    State = "error"
)

const (
	// ctxSize and the single slot keep the model's memory predictable.
	ctxSize = 4096
	// memHeadroom is what must be free on top of the model file.
	memHeadroom = 512 << 20
	// maxCaptured is how much of the child's output is kept, in memory only,
	// to explain a failed start.
	maxCaptured = 2048
)

// SupervisorConfig describes one sidecar.
type SupervisorConfig struct {
	ServerPath   string        // empty: look for llama-server in PATH
	ModelPath    string        // the .gguf the user chose
	RunDir       string        // private runtime dir for the key file and pid file
	Idle         time.Duration // stop after this long without a request
	StartTimeout time.Duration // loading the model, from a cold disk
	TermGrace    time.Duration // SIGTERM to SIGKILL
	Dial         time.Duration
	Response     time.Duration
}

// Supervisor owns one llama-server child (ADR 8): it starts it on demand,
// waits for /health, stops it after an idle period and on request, and
// never lets it outlive the daemon.
type Supervisor struct {
	cfg SupervisorConfig

	startMu sync.Mutex // one start or stop at a time

	mu          sync.Mutex
	state       State
	lastErr     string
	proc        *child
	inflight    int
	idle        *time.Timer
	closed      bool
	cancelStart context.CancelFunc
	help        map[string]string // server path -> its --help text
}

// NewSupervisor returns a supervisor that has started nothing.
func NewSupervisor(cfg SupervisorConfig) *Supervisor {
	if cfg.TermGrace <= 0 {
		cfg.TermGrace = 5 * time.Second
	}
	if cfg.StartTimeout <= 0 {
		cfg.StartTimeout = 60 * time.Second
	}
	if cfg.Dial <= 0 {
		cfg.Dial = 30 * time.Second
	}
	if cfg.Response <= 0 {
		cfg.Response = 120 * time.Second
	}
	return &Supervisor{cfg: cfg, state: StateOff, help: map[string]string{}}
}

// child is one running llama-server.
type child struct {
	cmd      *exec.Cmd
	done     chan struct{} // closed when the process has been reaped
	base     *url.URL      // http://127.0.0.1:port/v1
	root     *url.URL      // http://127.0.0.1:port, for /health
	key      string
	dir      string // private directory holding the key file
	out      *capture
	stopping atomic.Bool
}

// Lease is a running sidecar to send one request to.
type Lease struct {
	BaseURL *url.URL
	Key     string
	c       *child
	release func()
}

// Release marks the request finished; the idle timer starts when the last
// lease is released.
func (l *Lease) Release() { l.release() }

// Acquire returns a ready sidecar, starting it if needed.
func (s *Supervisor) Acquire(ctx context.Context) (*Lease, error) {
	s.startMu.Lock()
	defer s.startMu.Unlock()

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, ErrOff
	}
	c := s.proc
	s.mu.Unlock()

	if c != nil {
		// After a suspend or a crash the process may be gone or wedged.
		if s.alive(c) && s.healthy(ctx, c) == nil {
			return s.lease(c), nil
		}
		s.stopLocked()
	}
	c, err := s.start(ctx)
	if err != nil {
		s.mu.Lock()
		if errors.Is(err, ErrOff) {
			s.state = StateOff // stopped on purpose while starting
		} else {
			s.state, s.lastErr = StateError, err.Error()
		}
		s.mu.Unlock()
		return nil, err
	}
	return s.lease(c), nil
}

func (s *Supervisor) lease(c *child) *Lease {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inflight++
	if s.idle != nil {
		s.idle.Stop()
	}
	var once sync.Once
	return &Lease{BaseURL: c.base, Key: c.key, c: c, release: func() {
		once.Do(func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			s.inflight--
			if s.inflight == 0 && s.proc == c && !s.closed && s.cfg.Idle > 0 {
				s.idle = time.AfterFunc(s.cfg.Idle, func() { s.idleStop(c) })
			}
		})
	}}
}

// idleStop stops c if nothing used it since the timer started.
func (s *Supervisor) idleStop(c *child) {
	s.startMu.Lock()
	defer s.startMu.Unlock()
	s.mu.Lock()
	if s.inflight > 0 || s.proc != c {
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()
	logf("stopping llama-server after an idle period")
	s.stopLocked()
}

// Invalidate stops the sidecar a failed request used, so the next Acquire
// starts a fresh one.
func (s *Supervisor) Invalidate(l *Lease) {
	s.startMu.Lock()
	defer s.startMu.Unlock()
	s.mu.Lock()
	same := s.proc == l.c
	s.mu.Unlock()
	if same {
		s.stopLocked()
	}
}

// Stop stops the child now. A start in progress is abandoned.
func (s *Supervisor) Stop() {
	s.mu.Lock()
	if s.cancelStart != nil {
		s.cancelStart()
	}
	s.mu.Unlock()
	s.startMu.Lock()
	defer s.startMu.Unlock()
	s.stopLocked()
}

// Close is Stop, and the supervisor refuses further requests.
func (s *Supervisor) Close() {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	s.Stop()
}

// Status is the state and the last error.
func (s *Supervisor) Status() (State, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state, s.lastErr
}

// stopLocked terminates the child. The caller holds startMu.
func (s *Supervisor) stopLocked() {
	s.mu.Lock()
	c := s.proc
	s.proc = nil
	if s.idle != nil {
		s.idle.Stop()
	}
	s.mu.Unlock()
	if c != nil {
		s.terminate(c)
	}
	// Off only once the process is gone and its files are removed.
	s.mu.Lock()
	if s.state != StateError {
		s.state = StateOff
	}
	s.mu.Unlock()
}

// terminate asks the child to exit, kills it after the grace period and
// removes its key and pid files.
func (s *Supervisor) terminate(c *child) {
	c.stopping.Store(true)
	if err := c.cmd.Process.Signal(sigTerm); err != nil {
		c.cmd.Process.Kill()
	}
	select {
	case <-c.done:
	case <-time.After(s.cfg.TermGrace):
		c.cmd.Process.Kill()
		select {
		case <-c.done:
		case <-time.After(5 * time.Second):
		}
	}
	c.cleanup(s.cfg.RunDir)
}

func (c *child) cleanup(runDir string) {
	if c.dir != "" {
		os.RemoveAll(c.dir)
	}
	removePidFile(runDir)
}

// onExit runs when the process has been reaped. A child that exits without
// having been asked to is an error the next request reports and recovers from.
func (s *Supervisor) onExit(c *child) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.proc != c {
		return
	}
	s.proc = nil
	if s.idle != nil {
		s.idle.Stop()
	}
	if !c.stopping.Load() {
		s.state = StateError
		s.lastErr = "llama-server exited unexpectedly" + c.out.suffix()
		c.cleanup(s.cfg.RunDir)
	}
}

func (s *Supervisor) alive(c *child) bool {
	select {
	case <-c.done:
		return false
	default:
		return true
	}
}

func (s *Supervisor) healthy(ctx context.Context, c *child) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	resp, err := roundTrip(ctx, "GET", c.root.JoinPath("health"), nil, nil, timeouts{dial: s.cfg.Dial}, nil)
	if err != nil {
		return err
	}
	if resp.status != 200 {
		return fmt.Errorf("health check answered HTTP %d", resp.status)
	}
	return nil
}

// FindServer resolves the llama-server binary: the configured path, or PATH.
func FindServer(path string) (string, error) {
	if path == "" {
		p, err := exec.LookPath("llama-server")
		if err != nil {
			return "", errors.New("llama-server was not found in PATH; install it or set its path")
		}
		return p, nil
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return "", errors.New("llama-server was not found at the configured path")
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o111 == 0 {
		return "", errors.New("the configured llama-server is not executable")
	}
	return path, nil
}

// FindModel checks the .gguf file and returns its size.
func FindModel(path string) (int64, error) {
	if path == "" {
		return 0, errors.New("no model file set")
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return 0, errors.New("the model file was not found")
	}
	if info.Size() == 0 {
		return 0, errors.New("the model file is empty")
	}
	return info.Size(), nil
}

// start launches the child and waits for /health. The caller holds startMu.
func (s *Supervisor) start(ctx context.Context) (*child, error) {
	server, err := FindServer(s.cfg.ServerPath)
	if err != nil {
		return nil, err
	}
	size, err := FindModel(s.cfg.ModelPath)
	if err != nil {
		return nil, err
	}
	if avail, ok := availableMemory(); ok && uint64(size)+memHeadroom > avail {
		return nil, fmt.Errorf("not enough free memory to load the model: about %d MB needed, %d MB available",
			(uint64(size)+memHeadroom)>>20, avail>>20)
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, ErrOff
	}
	s.cancelStart = cancel
	s.state, s.lastErr = StateStarting, ""
	s.mu.Unlock()

	reapOrphan(s.cfg.RunDir)
	help, err := s.helpText(ctx, server)
	if err != nil {
		return nil, err
	}
	// A key is mandatory: without one any local user could use the model.
	// Prefer a 0600 file; else the environment (readable by this user only,
	// unlike the command line, which any local user can read).
	var keyFile, keyEnv bool
	switch {
	case strings.Contains(help, "--api-key-file"):
		keyFile = true
	case strings.Contains(help, "LLAMA_API_KEY"):
		keyEnv = true
	default:
		return nil, errors.New("this llama-server cannot take an API key privately (no --api-key-file); update it")
	}

	key, err := randomKey()
	if err != nil {
		return nil, err
	}
	dir, err := privateTempDir(s.cfg.RunDir)
	if err != nil {
		return nil, err
	}
	port, err := freePort()
	if err != nil {
		os.RemoveAll(dir)
		return nil, err
	}

	args := []string{
		"--model", s.cfg.ModelPath,
		"--host", "127.0.0.1",
		"--port", fmt.Sprint(port),
		"--ctx-size", fmt.Sprint(ctxSize),
		"--parallel", "1",
		"--threads", fmt.Sprint(threadCount()),
		"--n-gpu-layers", "0",
	}
	// Hardening flags only newer builds have; used when --help lists them.
	for _, f := range []string{"--no-webui", "--offline", "--log-disable"} {
		if strings.Contains(help, f) {
			args = append(args, f)
		}
	}
	env := filterEnv(os.Environ(), "LLAMA_API_KEY")
	if keyFile {
		path := filepath.Join(dir, "api-key")
		if err := os.WriteFile(path, []byte(key), 0o600); err != nil {
			os.RemoveAll(dir)
			return nil, err
		}
		args = append(args, "--api-key-file", path)
	} else if keyEnv {
		env = append(env, "LLAMA_API_KEY="+key)
	}

	cmd := exec.Command(server, args...)
	cmd.Env = env
	cmd.SysProcAttr = procAttr()
	out := &capture{on: true}
	cmd.Stdout, cmd.Stderr = out, out
	c := &child{
		cmd:  cmd,
		done: make(chan struct{}),
		root: &url.URL{Scheme: "http", Host: net.JoinHostPort("127.0.0.1", fmt.Sprint(port)), Path: "/"},
		key:  key,
		dir:  dir,
		out:  out,
	}
	c.base = c.root.JoinPath("v1")

	started := make(chan error, 1)
	go func() {
		// On Linux the child gets Pdeathsig, which the kernel ties to the OS
		// thread that forked it, not to the process. A Go goroutine hops
		// between threads, and when its thread ends the child would be killed
		// early. So the goroutine that starts the child locks its thread and
		// stays on it until the child has been reaped: the child lives
		// exactly as long as that thread, and the thread lives until the
		// daemon dies (then the kernel kills the child) or the child exits.
		// A daemon that dies without cleanup is also covered by the pid file
		// (reapOrphan), for platforms without Pdeathsig.
		runtime.LockOSThread()
		if err := cmd.Start(); err != nil {
			started <- err
			return
		}
		started <- nil
		_ = cmd.Wait()
		close(c.done)
		s.onExit(c)
	}()
	if err := <-started; err != nil {
		os.RemoveAll(dir)
		return nil, fmt.Errorf("could not start llama-server: %w", err)
	}
	lowerPriority(cmd.Process.Pid)
	exe := server
	if r, err := filepath.EvalSymlinks(server); err == nil {
		exe = r
	}
	writePidFile(s.cfg.RunDir, cmd.Process.Pid, exe)
	logf("started llama-server (pid %d), waiting for the model to load", cmd.Process.Pid)

	waitCtx, stopWaiting := context.WithTimeout(ctx, s.cfg.StartTimeout) // loading only, not the --help probe
	err = s.waitReady(waitCtx, c)
	stopWaiting()
	if err != nil {
		s.terminate(c)
		return nil, err
	}
	c.out.stop() // after loading, nothing the child prints is kept
	s.mu.Lock()
	s.proc, s.state, s.lastErr = c, StateReady, ""
	s.mu.Unlock()
	logf("llama-server is ready")
	return c, nil
}

func (s *Supervisor) waitReady(ctx context.Context, c *child) error {
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-c.done:
			return errors.New("llama-server exited while loading the model" + c.out.suffix())
		case <-ctx.Done():
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return errors.New("timed out waiting for llama-server to load the model")
			}
			return ErrOff
		case <-tick.C:
		}
		// Our own child must still be there, so a stranger on the port
		// cannot pass for it.
		if s.healthy(ctx, c) == nil && s.alive(c) {
			return nil
		}
	}
}

// helpText is the server's --help output, cached per binary, used to decide
// which optional flags to pass. Checking the flags against the installed
// build beats guessing which build the user has.
func (s *Supervisor) helpText(ctx context.Context, server string) (string, error) {
	s.mu.Lock()
	h, ok := s.help[server]
	s.mu.Unlock()
	if ok {
		return h, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, server, "--help")
	var buf limitedBuffer
	cmd.Stdout, cmd.Stderr = &buf, &buf
	// --help exits non-zero on some builds; only the text matters.
	_ = cmd.Run()
	if buf.b.Len() == 0 {
		return "", errors.New("llama-server printed no help text; is that the right program?")
	}
	h = buf.b.String()
	s.mu.Lock()
	s.help[server] = h
	s.mu.Unlock()
	return h, nil
}

type limitedBuffer struct{ b bytes.Buffer }

func (l *limitedBuffer) Write(p []byte) (int, error) {
	if room := 256<<10 - l.b.Len(); room > 0 {
		l.b.Write(p[:min(len(p), room)])
	}
	return len(p), nil
}

// capture keeps the first bytes of the child's output while it loads, to
// explain a failed start, and discards everything after stop.
type capture struct {
	mu  sync.Mutex
	on  bool
	buf []byte
}

func (c *capture) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.on && len(c.buf) < maxCaptured {
		c.buf = append(c.buf, p[:min(len(p), maxCaptured-len(c.buf))]...)
	}
	return len(p), nil
}

func (c *capture) stop() {
	c.mu.Lock()
	c.on, c.buf = false, nil
	c.mu.Unlock()
}

// suffix is ": <last output line>" or "", for appending to an error.
func (c *capture) suffix() string {
	c.mu.Lock()
	text := string(c.buf)
	c.mu.Unlock()
	lines := strings.Split(strings.TrimSpace(text), "\n")
	last := cleanText(lines[len(lines)-1], 200)
	if last == "" {
		return ""
	}
	return ": " + last
}

func randomKey() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// privateTempDir makes a 0700 directory inside runDir (or the system temp
// directory when there is none).
func privateTempDir(runDir string) (string, error) {
	if runDir != "" {
		if err := os.MkdirAll(runDir, 0o700); err != nil {
			return "", err
		}
	}
	return os.MkdirTemp(runDir, "planner-")
}

func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

func threadCount() int { return max(1, min(4, runtime.NumCPU()/2)) }

func filterEnv(env []string, drop string) []string {
	out := make([]string, 0, len(env)+1)
	for _, kv := range env {
		if !strings.HasPrefix(kv, drop+"=") {
			out = append(out, kv)
		}
	}
	return out
}

// cleanText makes untrusted text safe to put in a prompt or an error: control
// characters and line breaks become spaces, runs of spaces collapse, and the
// result is cut to max runes.
func cleanText(s string, max int) string {
	var b strings.Builder
	space := true
	n := 0
	for _, r := range s {
		if unicode.IsControl(r) || unicode.IsSpace(r) || !unicode.IsPrint(r) {
			if !space {
				b.WriteByte(' ')
				space = true
				n++
			}
		} else {
			b.WriteRune(r)
			space = false
			n++
		}
		if n >= max {
			break
		}
	}
	return strings.TrimSpace(b.String())
}

func trimTo(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max-1]) + "…"
}
