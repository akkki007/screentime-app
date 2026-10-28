package rpc

import (
	"path/filepath"
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
