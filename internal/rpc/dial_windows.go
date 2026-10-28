//go:build windows

package rpc

import (
	"fmt"
	"os"
	"path/filepath"
)

// CheckSocket is the client half of S2/S3 on Windows: the socket's directory
// must be a real directory (not a symlink or junction) and, like the socket,
// owned by the current user. There are no mode bits to check; see
// perm_windows.go for what protects the directory instead.
func CheckSocket(path string) error {
	dir := filepath.Dir(path)

	dinfo, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !dinfo.IsDir() {
		return fmt.Errorf("%s is not a directory; refusing to connect", dir)
	}
	if !ownedByMe(dir, dinfo) {
		return fmt.Errorf("%s is not owned by you; refusing to connect", dir)
	}

	sinfo, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if sinfo.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("%s is not a socket; refusing to connect", path)
	}
	if !ownedByMe(path, sinfo) {
		return fmt.Errorf("%s is not owned by you; refusing to connect", path)
	}
	return nil
}
