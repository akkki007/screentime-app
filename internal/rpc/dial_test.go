package rpc

import (
	"net"
	"os"
	"path/filepath"
	"testing"
)

func tempSocket(t *testing.T, dirMode os.FileMode) (string, net.Listener) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "s")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, dirMode); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "daemon.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	return path, ln
}

func TestDialConnectsToOwnedSocket(t *testing.T) {
	path, ln := tempSocket(t, 0o700)
	go func() {
		if c, err := ln.Accept(); err == nil {
			c.Close()
		}
	}()
	conn, err := Dial(path)
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()
}

func TestCheckSocketRefusesOpenDirectory(t *testing.T) {
	path, _ := tempSocket(t, 0o777)
	if err := CheckSocket(path); err == nil {
		t.Fatal("a world-writable socket directory must be refused")
	}
}

func TestCheckSocketRefusesNonSocket(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "daemon.sock")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := CheckSocket(path); err == nil {
		t.Fatal("a regular file must be refused")
	}
}

func TestCheckSocketRefusesSymlinkedDirectory(t *testing.T) {
	real, _ := tempSocket(t, 0o700)
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(filepath.Dir(real), link); err != nil {
		t.Fatal(err)
	}
	if err := CheckSocket(filepath.Join(link, "daemon.sock")); err == nil {
		t.Fatal("a symlinked directory must be refused")
	}
}

func TestCheckSocketMissing(t *testing.T) {
	if err := CheckSocket(filepath.Join(t.TempDir(), "nope", "daemon.sock")); err == nil {
		t.Fatal("a missing socket must be an error")
	}
}
