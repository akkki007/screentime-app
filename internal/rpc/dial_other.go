//go:build !linux

package rpc

import (
	"net"
	"os"
)

// Other platforms have no portable peer-credential call here; the ownership
// checks in CheckSocket still apply.
func peerUID(*net.UnixConn) (int, error) { return os.Getuid(), nil }
