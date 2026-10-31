//go:build unix

package rpc

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestSocketIsPrivate(t *testing.T) {
	old := syscall.Umask(0o022)
	defer syscall.Umask(old)
	_, path, _ := start(t)
	for p, want := range map[string]os.FileMode{path: 0o600, filepath.Dir(path): 0o700} {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != want {
			t.Errorf("%s mode = %o, want %o", p, got, want)
		}
	}
}

// S2: the socket directory must be a real directory owned by this user.
func TestRefusesUnsafeSocketDir(t *testing.T) {
	t.Run("symlink", func(t *testing.T) {
		base := shortDir(t)
		target := filepath.Join(base, "elsewhere")
		os.Mkdir(target, 0o700)
		os.Symlink(target, filepath.Join(base, "screentime"))
		if _, err := Listen(filepath.Join(base, "screentime", "daemon.sock"), &fakeHandler{}); err == nil {
			t.Error("listened through a symlinked directory")
		}
	})
	t.Run("loose mode is tightened", func(t *testing.T) {
		base := shortDir(t)
		dir := filepath.Join(base, "screentime")
		os.Mkdir(dir, 0o777)
		os.Chmod(dir, 0o777)
		s, err := Listen(filepath.Join(dir, "daemon.sock"), &fakeHandler{})
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		info, _ := os.Stat(dir)
		if info.Mode().Perm() != 0o700 {
			t.Errorf("dir mode = %o, want 700", info.Mode().Perm())
		}
	})
	t.Run("a non-socket file is not replaced", func(t *testing.T) {
		base := shortDir(t)
		dir := filepath.Join(base, "screentime")
		os.Mkdir(dir, 0o700)
		path := filepath.Join(dir, "daemon.sock")
		os.WriteFile(path, []byte("keep me"), 0o600)
		if _, err := Listen(path, &fakeHandler{}); err == nil {
			t.Error("replaced a regular file")
		}
		if b, _ := os.ReadFile(path); string(b) != "keep me" {
			t.Error("regular file was modified")
		}
	})
	t.Run("a stale socket is replaced", func(t *testing.T) {
		base := shortDir(t)
		path := filepath.Join(base, "screentime", "daemon.sock")
		first, err := Listen(path, &fakeHandler{})
		if err != nil {
			t.Fatal(err)
		}
		// Simulate a crash: the socket file stays behind with nobody listening.
		first.listener.SetUnlinkOnClose(false)
		first.Close()
		second, err := Listen(path, &fakeHandler{})
		if err != nil {
			t.Fatalf("stale socket not replaced: %v", err)
		}
		second.Close()
	})
}
