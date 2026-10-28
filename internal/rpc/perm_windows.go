//go:build windows

package rpc

import (
	"net"
	"os"

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
// than the user, so a daemon started from an elevated shell would fail the
// check; it is meant to run as the logged-in user.

// ownedByMe reports whether the owner of path is the current user.
func ownedByMe(path string, _ os.FileInfo) bool {
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION)
	if err != nil {
		return false
	}
	owner, _, err := sd.Owner()
	if err != nil || owner == nil {
		return false
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return false
	}
	return owner.Equals(user.User.Sid)
}

// tightenDir does nothing: Windows reports synthetic mode bits, and the
// directory's ACL is inherited from %LOCALAPPDATA%.
func tightenDir(string, os.FileInfo) error { return nil }

func listenPrivate(path string) (*net.UnixListener, error) {
	return net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
}
