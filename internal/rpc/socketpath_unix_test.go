//go:build unix && !darwin

package rpc

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSocketPathIgnoresUntrustedRuntimeDir(t *testing.T) {
	loose := shortDir(t)
	os.Chmod(loose, 0o777)
	t.Setenv("XDG_RUNTIME_DIR", loose)
	if got := SocketPath(); strings.HasPrefix(got, loose) {
		t.Errorf("used world-writable XDG_RUNTIME_DIR: %s", got)
	}
	private := shortDir(t)
	os.Chmod(private, 0o700)
	t.Setenv("XDG_RUNTIME_DIR", private)
	if got, want := SocketPath(), filepath.Join(private, "screentime", "daemon.sock"); got != want {
		t.Errorf("SocketPath = %s, want %s", got, want)
	}
}
