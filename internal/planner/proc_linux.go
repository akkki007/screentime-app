//go:build linux

package planner

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// procAttr makes the kernel kill the child when its parent thread dies. The
// supervisor starts the child from a goroutine locked to one OS thread for
// the child's whole life (see Supervisor.start), which is what makes this
// mean "when the daemon dies".
func procAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
}

const (
	ioprioWhoProcess = 1
	ioprioClassIdle  = 3
	ioprioShift      = 13
)

// lowerPriority is best effort: nice 19 and the idle I/O class, so the model
// yields to whatever the user is doing. Failures are ignored.
func lowerPriority(pid int) {
	_ = syscall.Setpriority(syscall.PRIO_PROCESS, pid, 19)
	_, _, _ = syscall.Syscall(syscall.SYS_IOPRIO_SET, ioprioWhoProcess, uintptr(pid), ioprioClassIdle<<ioprioShift)
}

// availableMemory is MemAvailable from /proc/meminfo, in bytes.
func availableMemory() (uint64, bool) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		rest, ok := strings.CutPrefix(sc.Text(), "MemAvailable:")
		if !ok {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) == 0 {
			return 0, false
		}
		kb, err := strconv.ParseUint(fields[0], 10, 64)
		return kb << 10, err == nil
	}
	return 0, false
}

// reapOrphan kills a llama-server left behind by a daemon that died without
// cleaning up. The pid file records the pid and the binary; the process is
// only killed if /proc/<pid>/exe is still that binary, so a reused pid is
// left alone.
func reapOrphan(runDir string) {
	pid, exe, ok := readPidFile(runDir)
	if !ok {
		return
	}
	defer removePidFile(runDir)
	got, err := os.Readlink("/proc/" + strconv.Itoa(pid) + "/exe")
	if err != nil || strings.TrimSuffix(got, " (deleted)") != exe {
		return
	}
	if p, err := os.FindProcess(pid); err == nil {
		_ = p.Kill()
		logf("stopped a llama-server left over from an earlier run (pid %d)", pid)
	}
}

func pidFilePath(runDir string) string { return filepath.Join(runDir, "planner.pid") }
