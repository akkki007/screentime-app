package rpc

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// CheckSocket is the client half of S2/S3: before talking to a socket, make
// sure it and its directory belong to the current user, so a process that
// can write to a shared runtime directory can't pose as the daemon. The
// directory must also be closed to everyone else.
func CheckSocket(path string) error {
	uid := os.Getuid()
	dir := filepath.Dir(path)

	dinfo, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !dinfo.IsDir() {
		return fmt.Errorf("%s is not a directory; refusing to connect", dir)
	}
	if !ownedBy(dinfo, uid) {
		return fmt.Errorf("%s is not owned by you; refusing to connect", dir)
	}
	if dinfo.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("%s is writable by others (mode %o); refusing to connect", dir, dinfo.Mode().Perm())
	}

	sinfo, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if sinfo.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("%s is not a socket; refusing to connect", path)
	}
	if !ownedBy(sinfo, uid) {
		return fmt.Errorf("%s is not owned by you; refusing to connect", path)
	}
	return nil
}

func ownedBy(info os.FileInfo, uid int) bool {
	st, ok := info.Sys().(*syscall.Stat_t)
	return ok && int(st.Uid) == uid
}

// Dial connects to the daemon at path after CheckSocket, and then confirms
// the process on the other end runs as the current user. Shared by the
// native-messaging host and the desktop shell.
func Dial(path string) (*net.UnixConn, error) {
	if err := CheckSocket(path); err != nil {
		return nil, err
	}
	conn, err := net.DialTimeout("unix", path, 2*time.Second)
	if err != nil {
		return nil, err
	}
	uc := conn.(*net.UnixConn)
	if uid, err := peerUID(uc); err != nil {
		uc.Close()
		return nil, fmt.Errorf("checking daemon identity: %w", err)
	} else if uid != os.Getuid() {
		uc.Close()
		return nil, errors.New("the socket is served by another user; refusing to connect")
	}
	return uc, nil
}
