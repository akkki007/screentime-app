//go:build windows

package rpc

import (
	"errors"
	"fmt"
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
// than the user (measured on GitHub's Windows runner: owner S-1-5-32-544, user
// a RID-500 account), so "owned by me" accepts either the user or, when this
// process is itself an administrator, the Administrators group. A standard
// user, or an administrator who is not elevated, creates files owned by the
// user, so for them nothing is loosened.

// ownedByMe reports whether the owner of path is the current user, or the
// Administrators group when the current process is an administrator.
func ownedByMe(path string, _ os.FileInfo) bool {
	owner, err := ownerOf(path)
	if err != nil {
		return false
	}
	token := windows.GetCurrentProcessToken()
	if user, err := token.GetTokenUser(); err == nil && owner.Equals(user.User.Sid) {
		return true
	}
	return isAdminOwner(owner, token)
}

// isAdminOwner is whether owner is the Administrators group and token
// belongs to it (an elevated administrator: IsMember is false for the
// deny-only group of a non-elevated one).
func isAdminOwner(owner *windows.SID, token windows.Token) bool {
	admins, err := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid)
	if err != nil || !owner.Equals(admins) {
		return false
	}
	member, err := token.IsMember(admins)
	return err == nil && member
}

func ownerOf(path string) (*windows.SID, error) {
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION)
	if err != nil {
		return nil, err
	}
	owner, _, err := sd.Owner()
	if err != nil || owner == nil {
		return nil, errors.New("no owner")
	}
	return owner, nil
}

// tightenDir does nothing: Windows reports synthetic mode bits, and the
// directory's ACL is inherited from %LOCALAPPDATA%.
func tightenDir(string, os.FileInfo) error { return nil }

func listenPrivate(path string) (*net.UnixListener, error) {
	return net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
}

// ownerDetail names the SIDs involved, for an ownership error: the owner of
// path, the current user and whether this process is an administrator. They
// are local account identifiers, not secrets, and they are what is needed to
// see why a directory the user just created was refused.
func ownerDetail(path string) string {
	owner, err := ownerOf(path)
	if err != nil {
		return " (owner unknown: " + err.Error() + ")"
	}
	token := windows.GetCurrentProcessToken()
	you := "unknown"
	if user, err := token.GetTokenUser(); err == nil {
		you = user.User.Sid.String()
	}
	admin := false
	if admins, err := windows.CreateWellKnownSid(windows.WinBuiltinAdministratorsSid); err == nil {
		admin, _ = token.IsMember(admins)
	}
	return " (owner " + owner.String() + ", you " + you + ", administrator " + fmt.Sprint(admin) + ")"
}
