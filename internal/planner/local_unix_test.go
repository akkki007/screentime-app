//go:build unix

package planner

import (
	"bytes"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestLocalLifecycleStartReadyIdleStopRestart(t *testing.T) {
	var logs bytes.Buffer
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })

	db := openDB(t)
	p, startLog := localPlanner(t, db, 400*time.Millisecond)

	// Nothing runs until a request arrives.
	if st := p.Status(); st.State != "off" || !st.ServerFound || !st.ModelFound || st.LastError != nil {
		t.Fatalf("before any request: %+v", st)
	}
	if countStarts(t, startLog) != 0 {
		t.Fatal("the sidecar started without a request")
	}

	if _, err := p.Suggest(""); err != nil {
		t.Fatal(err)
	}
	if st := p.Status(); st.State != "ready" {
		t.Errorf("after a request: %+v", st)
	}
	if n := countStarts(t, startLog); n != 1 {
		t.Fatalf("%d starts", n)
	}

	// How it was started.
	args, _ := os.ReadFile(startLog + ".args")
	lines := strings.Split(string(args), "\n")
	has := func(flag, value string) bool {
		for i, l := range lines {
			if l == flag && (value == "" || (i+1 < len(lines) && lines[i+1] == value)) {
				return true
			}
		}
		return false
	}
	for _, want := range [][2]string{
		{"--host", "127.0.0.1"}, {"--parallel", "1"}, {"--ctx-size", "4096"}, {"--n-gpu-layers", "0"},
		{"--no-webui", ""}, {"--offline", ""}, {"--api-key-file", ""},
	} {
		if !has(want[0], want[1]) {
			t.Errorf("missing %v in %q", want, lines)
		}
	}
	if has("--api-key", "") {
		t.Error("the key is on the command line")
	}
	p.mu.Lock()
	key := p.sup.proc.key
	p.mu.Unlock()
	if len(key) < 32 || strings.Contains(string(args), key) || strings.Contains(logs.String(), key) {
		t.Error("the sidecar key is short, on the command line or in the log")
	}
	if _, err := os.Stat(pidFilePath(p.cfg.RunDir)); err != nil {
		t.Errorf("no pid file: %v", err)
	}

	// Idle shutdown.
	waitFor(t, "the idle stop", func() bool { return p.Status().State == "off" })
	if entries, _ := filepath.Glob(filepath.Join(p.cfg.RunDir, "*")); len(entries) != 0 {
		t.Errorf("files left behind after stopping: %v", entries)
	}

	// And it comes back on demand.
	if _, err := p.Suggest(""); err != nil {
		t.Fatal(err)
	}
	if n := countStarts(t, startLog); n != 2 {
		t.Errorf("%d starts, want 2", n)
	}
}

func TestBusySidecarIsNotStoppedByTheIdleTimer(t *testing.T) {
	db := openDB(t)
	p, startLog := localPlanner(t, db, 50*time.Millisecond)
	for range 3 {
		if _, err := p.Suggest(""); err != nil {
			t.Fatal(err)
		}
		time.Sleep(10 * time.Millisecond) // well inside the idle period
	}
	if n := countStarts(t, startLog); n != 1 {
		t.Errorf("%d starts for back-to-back requests", n)
	}
}

func TestStopStopsTheSidecarNow(t *testing.T) {
	db := openDB(t)
	p, startLog := localPlanner(t, db, time.Hour)
	if _, err := p.Suggest(""); err != nil {
		t.Fatal(err)
	}
	pid := lastPid(t, startLog)
	p.Stop()
	if st := p.Status(); st.State != "off" {
		t.Errorf("status = %+v", st)
	}
	waitFor(t, "the process to exit", func() bool { return !processAlive(pid) })
	p.Stop() // stopping a stopped planner is fine
	if _, err := p.Suggest(""); err != nil {
		t.Fatal(err)
	}
	if n := countStarts(t, startLog); n != 2 {
		t.Errorf("%d starts, want 2", n)
	}
}

func TestCloseStopsTheSidecarAndWaits(t *testing.T) {
	db := openDB(t)
	p, startLog := localPlanner(t, db, time.Hour)
	if _, err := p.Suggest(""); err != nil {
		t.Fatal(err)
	}
	pid := lastPid(t, startLog)
	p.Close()
	if processAlive(pid) {
		t.Error("llama-server outlived Close")
	}
}

func TestConfigureStopsASidecarThatNoLongerApplies(t *testing.T) {
	db := openDB(t)
	p, startLog := localPlanner(t, db, time.Hour)
	if _, err := p.Suggest(""); err != nil {
		t.Fatal(err)
	}
	pid := lastPid(t, startLog)
	o := p.options()
	o.ModelPath += ".other"
	p.Configure(o)
	waitFor(t, "the old sidecar to stop", func() bool { return !processAlive(pid) })
	if st := p.Status(); st.State != "error" || st.ModelFound {
		t.Errorf("status = %+v", st)
	}
}

func TestCrashMidRequestIsRetriedOnce(t *testing.T) {
	db := openDB(t)
	t.Setenv("FAKE_LLAMA_CRASH", filepath.Join(t.TempDir(), "crashed"))
	p, startLog := localPlanner(t, db, time.Hour)
	if _, err := p.Suggest(""); err != nil {
		t.Fatal(err)
	}
	if n := countStarts(t, startLog); n != 2 {
		t.Errorf("%d starts, want 2 (the first one crashed)", n)
	}
	if st := p.Status(); st.State != "ready" || st.LastError != nil {
		t.Errorf("status = %+v", st)
	}
}

func TestSidecarThatDiedWhileIdleIsRestarted(t *testing.T) {
	db := openDB(t)
	p, startLog := localPlanner(t, db, time.Hour)
	if _, err := p.Suggest(""); err != nil {
		t.Fatal(err)
	}
	// As after a suspend: the process is gone behind the supervisor's back.
	pid := lastPid(t, startLog)
	proc, _ := os.FindProcess(pid)
	proc.Kill()
	waitFor(t, "the exit to be noticed", func() bool { return p.Status().State == "error" })
	if st := p.Status(); st.LastError == nil || !strings.Contains(*st.LastError, "exited") {
		t.Errorf("status = %+v", st)
	}
	if _, err := p.Suggest(""); err != nil {
		t.Fatal(err)
	}
	if n := countStarts(t, startLog); n != 2 {
		t.Errorf("%d starts, want 2", n)
	}
}

func TestMissingServerOrModelStartsNothing(t *testing.T) {
	db := openDB(t)
	p, startLog := localPlanner(t, db, time.Hour)
	good := p.options()

	p.Configure(Options{Provider: "local", ServerPath: "/nonexistent/llama-server", ModelPath: good.ModelPath})
	st := p.Status()
	if st.State != "error" || st.ServerFound || !st.ModelFound || st.LastError == nil {
		t.Errorf("no server: %+v", st)
	}
	if _, err := p.Suggest(""); err == nil || !strings.Contains(err.Error(), "llama-server") {
		t.Errorf("err = %v", err)
	}

	p.Configure(Options{Provider: "local", ServerPath: good.ServerPath, ModelPath: "/nonexistent/m.gguf"})
	st = p.Status()
	if st.State != "error" || !st.ServerFound || st.ModelFound {
		t.Errorf("no model: %+v", st)
	}
	if _, err := p.Suggest(""); err == nil || !strings.Contains(err.Error(), "model") {
		t.Errorf("err = %v", err)
	}
	if countStarts(t, startLog) != 0 {
		t.Error("a process was started")
	}
}

func TestServerWithoutPrivateKeySupportIsRefused(t *testing.T) {
	db := openDB(t)
	t.Setenv("FAKE_LLAMA_HELP", "none")
	p, startLog := localPlanner(t, db, time.Hour)
	_, err := p.Suggest("")
	if err == nil || !strings.Contains(err.Error(), "API key privately") {
		t.Fatalf("err = %v", err)
	}
	if countStarts(t, startLog) != 0 {
		t.Error("a process was started without a key")
	}
	if st := p.Status(); st.State != "error" {
		t.Errorf("status = %+v", st)
	}
}

func TestServerThatOnlyTakesTheKeyFromTheEnvironment(t *testing.T) {
	db := openDB(t)
	t.Setenv("FAKE_LLAMA_HELP", "env")
	p, startLog := localPlanner(t, db, time.Hour)
	if _, err := p.Suggest(""); err != nil {
		t.Fatal(err)
	}
	args, _ := os.ReadFile(startLog + ".args")
	p.mu.Lock()
	key := p.sup.proc.key
	p.mu.Unlock()
	if strings.Contains(string(args), key) || strings.Contains(string(args), "--api-key") {
		t.Errorf("key on the command line: %q", args)
	}
	if !strings.Contains(string(args), "--no-webui") {
		t.Error("expected the optional hardening flag the build lists")
	}
}

func TestSlowModelLoadWaitsForHealth(t *testing.T) {
	db := openDB(t)
	t.Setenv("FAKE_LLAMA_LOAD", "700")
	p, _ := localPlanner(t, db, time.Hour)
	start := time.Now()
	if _, err := p.Suggest(""); err != nil {
		t.Fatal(err)
	}
	if time.Since(start) < 600*time.Millisecond {
		t.Error("answered before the model had loaded")
	}
}

func TestStartTimeoutKillsTheChild(t *testing.T) {
	db := openDB(t)
	t.Setenv("FAKE_LLAMA_LOAD", "60000")
	p, startLog := localPlanner(t, db, time.Hour)
	p.tm.start = 600 * time.Millisecond
	_, err := p.Suggest("")
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("err = %v", err)
	}
	pid := lastPid(t, startLog)
	waitFor(t, "the child to be killed", func() bool { return !processAlive(pid) })
	if st := p.Status(); st.State != "error" {
		t.Errorf("status = %+v", st)
	}
}

func TestStopDuringStartAbandonsIt(t *testing.T) {
	db := openDB(t)
	t.Setenv("FAKE_LLAMA_LOAD", "60000")
	p, startLog := localPlanner(t, db, time.Hour)
	done := make(chan error, 1)
	go func() { _, err := p.Suggest(""); done <- err }()
	waitFor(t, "the child to start", func() bool { return countStarts(t, startLog) == 1 })
	p.Stop()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("the request was not abandoned")
	}
	if st := p.Status(); st.State != "off" {
		t.Errorf("status = %+v", st)
	}
	waitFor(t, "the child to exit", func() bool { return !processAlive(lastPid(t, startLog)) })
}

func lastPid(t *testing.T, startLog string) int {
	t.Helper()
	b, _ := os.ReadFile(startLog)
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	var pid int
	if len(lines) == 0 || !scan(lines[len(lines)-1], &pid) {
		t.Fatalf("no pid in %q", b)
	}
	return pid
}

func scan(line string, pid *int) bool {
	_, rest, _ := strings.Cut(line, " ")
	n, err := strconv.Atoi(rest)
	*pid = n
	return err == nil && n > 0
}

// processAlive reports whether pid still exists and is not a zombie.
func processAlive(pid int) bool {
	out, err := exec.Command("ps", "-o", "stat=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return false // no such process
	}
	s := strings.TrimSpace(string(out))
	return s != "" && !strings.HasPrefix(s, "Z")
}
