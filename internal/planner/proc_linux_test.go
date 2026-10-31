//go:build linux

package planner

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// fakeOrphan starts the test binary as a stand-in for a llama-server that
// outlived its daemon.
func fakeOrphan(t *testing.T) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(os.Args[0], "--port", "0")
	cmd.Env = append(os.Environ(), "FAKE_LLAMA_SERVER=1")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	return cmd
}

func TestReapOrphanKillsOnlyTheRecordedBinary(t *testing.T) {
	runDir := t.TempDir()
	exe, err := filepath.EvalSymlinks(os.Args[0])
	if err != nil {
		t.Fatal(err)
	}

	// A pid reused by something else: the pid file names a different binary.
	other := fakeOrphan(t)
	writePidFile(runDir, other.Process.Pid, "/usr/bin/something-else")
	reapOrphan(runDir)
	time.Sleep(100 * time.Millisecond)
	if !processAlive(other.Process.Pid) {
		t.Error("killed a process that is not llama-server")
	}
	if _, err := os.Stat(pidFilePath(runDir)); err == nil {
		t.Error("stale pid file kept")
	}

	// A real leftover.
	orphan := fakeOrphan(t)
	writePidFile(runDir, orphan.Process.Pid, exe)
	reapOrphan(runDir)
	done := make(chan struct{})
	go func() { orphan.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Error("the orphan survived")
	}
	if _, err := os.Stat(pidFilePath(runDir)); err == nil {
		t.Error("pid file kept after reaping")
	}
}

func TestNotEnoughMemoryRefusesToStart(t *testing.T) {
	avail, ok := availableMemory()
	if !ok {
		t.Skip("no MemAvailable")
	}
	db := openDB(t)
	p, startLog := localPlanner(t, db, time.Hour)
	model := p.options().ModelPath
	if err := os.Truncate(model, int64(avail)+1<<30); err != nil { // sparse
		t.Skipf("cannot make a large sparse file: %v", err)
	}
	_, err := p.Suggest("")
	if err == nil || !strings.Contains(err.Error(), "not enough free memory") {
		t.Fatalf("err = %v", err)
	}
	if countStarts(t, startLog) != 0 {
		t.Error("started anyway")
	}
}

func TestPriorityIsLowered(t *testing.T) {
	db := openDB(t)
	p, startLog := localPlanner(t, db, time.Hour)
	if _, err := p.Suggest(""); err != nil {
		t.Fatal(err)
	}
	pid := lastPid(t, startLog)
	stat, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		t.Skip(err)
	}
	// Field 19 of /proc/<pid>/stat is the nice value (after the "(comm)" field).
	rest := string(stat[strings.LastIndexByte(string(stat), ')')+2:])
	fields := strings.Fields(rest)
	if len(fields) < 17 || fields[16] != "19" {
		t.Errorf("nice = %v", fields[min(16, len(fields)-1)])
	}
}
