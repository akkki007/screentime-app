package rpc

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestDarwinSocketPath(t *testing.T) {
	got := darwinSocketPath("/Users/ada")
	want := filepath.Join("/Users", "ada", "Library", "Caches", "screentime", "daemon.sock")
	if got != want {
		t.Errorf("darwinSocketPath = %s, want %s", got, want)
	}
}

func TestWindowsSocketPath(t *testing.T) {
	local := filepath.Join("C:", "Users", "ada", "AppData", "Local")
	if got, want := windowsSocketPath(local, "ignored"), filepath.Join(local, "screentime", "daemon.sock"); got != want {
		t.Errorf("windowsSocketPath = %s, want %s", got, want)
	}
	// Without LOCALAPPDATA it falls back to where that variable normally points.
	home := filepath.Join("C:", "Users", "ada")
	if got, want := windowsSocketPath("", home), filepath.Join(home, "AppData", "Local", "screentime", "daemon.sock"); got != want {
		t.Errorf("fallback = %s, want %s", got, want)
	}
}

func TestSocketPathLengthIsCheckedWithAClearError(t *testing.T) {
	short := "/tmp/screentime/daemon.sock"
	if err := checkPathLength(short); err != nil {
		t.Errorf("short path refused: %v", err)
	}
	long := "/home/" + strings.Repeat("x", 200) + "/daemon.sock"
	err := checkPathLength(long)
	if err == nil || !strings.Contains(err.Error(), "over the") {
		t.Fatalf("a path over the limit gave %v", err)
	}
	if _, err := Listen(long, nil); err == nil || !strings.Contains(err.Error(), "socket path is") {
		t.Errorf("Listen on a long path: %v", err)
	}
}
