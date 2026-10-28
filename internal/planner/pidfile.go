package planner

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
)

// sigTerm is how the child is asked to exit. Windows cannot deliver it, and
// the supervisor falls back to Kill when Signal fails.
var sigTerm os.Signal = syscall.SIGTERM

// The pid file lets the next daemon find a sidecar that outlived a killed
// daemon. It lives in the 0700 runtime directory next to the RPC socket.

func writePidFile(runDir string, pid int, exe string) {
	if runDir == "" {
		return
	}
	_ = os.WriteFile(pidFilePath(runDir), []byte(fmt.Sprintf("%d\n%s\n", pid, exe)), 0o600)
}

func readPidFile(runDir string) (pid int, exe string, ok bool) {
	if runDir == "" {
		return 0, "", false
	}
	b, err := os.ReadFile(pidFilePath(runDir))
	if err != nil {
		return 0, "", false
	}
	first, rest, _ := strings.Cut(string(b), "\n")
	pid, err = strconv.Atoi(strings.TrimSpace(first))
	exe = strings.TrimSpace(rest)
	return pid, exe, err == nil && pid > 1 && exe != ""
}

func removePidFile(runDir string) {
	if runDir != "" {
		_ = os.Remove(pidFilePath(runDir))
	}
}
