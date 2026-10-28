//go:build darwin

package rpc

import "os"

// SocketPath is ~/Library/Caches/screentime/daemon.sock (darwinSocketPath).
// macOS has no XDG_RUNTIME_DIR; the directory is made 0700 and checked like
// the Linux one (S2).
func SocketPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = os.Getenv("HOME")
	}
	return darwinSocketPath(home)
}
