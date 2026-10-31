//go:build unix && !darwin

package rpc

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"syscall"
)

// SocketPath is $XDG_RUNTIME_DIR/screentime/daemon.sock. XDG_RUNTIME_DIR is
// trusted only if it is a directory owned by this user with mode 0700 (as
// GLib requires); otherwise /run/user/<uid> is used.
func SocketPath() string {
	base := os.Getenv("XDG_RUNTIME_DIR")
	if base == "" || !privateTo(base, os.Getuid()) {
		if base != "" {
			log.Printf("[rpc] ignoring XDG_RUNTIME_DIR=%s: not a 0700 directory owned by this user", base)
		}
		base = fmt.Sprintf("/run/user/%d", os.Getuid())
	}
	return filepath.Join(base, "screentime", "daemon.sock")
}

func privateTo(dir string, uid int) bool {
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() {
		return false
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	return ok && int(st.Uid) == uid && info.Mode().Perm()&0o077 == 0
}
