package rpc

import (
	"errors"
	"fmt"
	"net"
	"os"
	"time"
)

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
