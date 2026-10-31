//go:build windows

package rpc

import (
	"net"
	"os"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Windows has no uid, mode bits or umask, so S2/S3 are met differently:
//
//   - The socket lives in %LOCALAPPDATA%\screentime, a per-user directory
//     whose inherited ACL grants access to that user, SYSTEM and the
//     administrators only. That is the access control: another standard user
//     can neither list the directory nor reach the socket inside it. (A local
//     administrator can read everything the user can; so can they on Linux
//     as root.)
//   - Like the Unix check, the directory and the socket must be owned by the
//     current user (ownedByMe), which keeps a process that could write to a
//     shared location from planting a socket.
//   - There is no mode to tighten and no peer-credential check on the
//     connection (dial_other.go), so SO_PEERCRED's confirmation of who
//     serves the socket is not available; the owner check on the socket
//     file is what remains.
//
// An elevated process creates files owned by the Administrators group rather
// than the user (the token's default owner), so "owned by me" accepts either
// the user or the owner the token gives new objects. Without that a daemon
// started from an elevated shell, or by a user whose account policy makes
// Administrators the default owner, would refuse its own directory.

// ownedByMe reports whether the owner of path is the current user, or the
// owner this process's new files get.
func ownedByMe(path string, _ os.FileInfo) bool {
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION)
	if err != nil {
		return false
	}
	owner, _, err := sd.Owner()
	if err != nil || owner == nil {
		return false
	}
	token := windows.GetCurrentProcessToken()
	if user, err := token.GetTokenUser(); err == nil && owner.Equals(user.User.Sid) {
		return true
	}
	def, err := defaultOwner(token)
	return err == nil && owner.Equals(def)
}

// defaultOwner is the SID the token gives to objects it creates (TokenOwner).
func defaultOwner(token windows.Token) (*windows.SID, error) {
	var n uint32
	err := windows.GetTokenInformation(token, windows.TokenOwner, nil, 0, &n)
	if err != windows.ERROR_INSUFFICIENT_BUFFER {
		return nil, err
	}
	buf := make([]byte, n)
	if err := windows.GetTokenInformation(token, windows.TokenOwner, &buf[0], n, &n); err != nil {
		return nil, err
	}
	// A TOKEN_OWNER is a struct holding one SID pointer, into buf.
	return (*tokenOwner)(unsafe.Pointer(&buf[0])).owner, nil
}

type tokenOwner struct{ owner *windows.SID }

// tightenDir does nothing: Windows reports synthetic mode bits, and the
// directory's ACL is inherited from %LOCALAPPDATA%.
func tightenDir(string, os.FileInfo) error { return nil }

func listenPrivate(path string) (*net.UnixListener, error) {
	return net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
}
