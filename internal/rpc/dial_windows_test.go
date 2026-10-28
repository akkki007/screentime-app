//go:build windows

package rpc

import (
	"os"
	"path/filepath"
	"testing"
)

// A socket this user created passes the owner checks and can be dialled.
func TestDialConnectsToOwnedSocket(t *testing.T) {
	_, path, _ := start(t)
	conn, err := Dial(path)
	if err != nil {
		t.Fatal(err)
	}
	conn.Close()
}

func TestCheckSocketRefusesNonSocket(t *testing.T) {
	path := filepath.Join(t.TempDir(), "daemon.sock")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := CheckSocket(path); err == nil {
		t.Fatal("a regular file must be refused")
	}
}

func TestCheckSocketMissing(t *testing.T) {
	if err := CheckSocket(filepath.Join(t.TempDir(), "nope", "daemon.sock")); err == nil {
		t.Fatal("a missing socket must be an error")
	}
}
