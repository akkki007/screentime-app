//go:build !linux

package planner

import (
	"path/filepath"
	"syscall"
)

// Windows would use a Job Object with kill-on-close and macOS has no
// equivalent of Pdeathsig (ADR 8); neither is implemented yet. Until then
// the child is stopped by the daemon's shutdown path, and a llama-server
// that outlives a killed daemon is found by its pid file only on Linux.
func procAttr() *syscall.SysProcAttr { return nil }

func lowerPriority(pid int) {}

// availableMemory is not read outside Linux, so no memory guard applies.
func availableMemory() (uint64, bool) { return 0, false }

func reapOrphan(runDir string) { removePidFile(runDir) }

func pidFilePath(runDir string) string { return filepath.Join(runDir, "planner.pid") }
