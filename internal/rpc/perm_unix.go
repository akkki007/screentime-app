//go:build unix

package rpc

import (
	"log"
	"net"
	"os"
	"syscall"
)

// ownedByMe reports whether info (of path) belongs to the current user.
func ownedByMe(_ string, info os.FileInfo) bool { return ownedBy(info, os.Getuid()) }

// tightenDir removes group and other permissions from the socket directory.
func tightenDir(dir string, info os.FileInfo) error {
	if info.Mode().Perm()&0o077 != 0 {
		log.Printf("[rpc] %s was mode %o; setting 0700", dir, info.Mode().Perm())
		return os.Chmod(dir, 0o700)
	}
	return nil
}

// listenPrivate binds the socket. bind creates it as 0777 minus the umask:
// 0600 here, with no path-based chmod afterwards.
func listenPrivate(path string) (*net.UnixListener, error) {
	old := syscall.Umask(0o177)
	defer syscall.Umask(old)
	return net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
}
