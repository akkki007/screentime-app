package rpc

import "path/filepath"

// The per-OS defaults, kept free of build tags so they can be tested
// anywhere. SocketPath (socketpath_*.go) picks one.

// darwinSocketPath is ~/Library/Caches/screentime/daemon.sock: a per-user
// directory at a fixed, short path. $TMPDIR is per-user on macOS too, but it
// differs between launchd, a terminal and a browser's native-messaging
// launch, and the daemon and its clients must agree. A Unix socket path may
// be at most 103 bytes on macOS; this is about 45 plus the user name.
func darwinSocketPath(home string) string {
	return filepath.Join(home, "Library", "Caches", "screentime", "daemon.sock")
}

// windowsSocketPath is %LOCALAPPDATA%\screentime\daemon.sock, next to the
// database: a per-user directory that other standard users can't read. Without
// LOCALAPPDATA (a stripped environment) it is where that variable points.
func windowsSocketPath(localAppData, home string) string {
	return filepath.Join(windowsDataDir(localAppData, home), "daemon.sock")
}

func windowsDataDir(localAppData, home string) string {
	if localAppData == "" {
		localAppData = filepath.Join(home, "AppData", "Local")
	}
	return filepath.Join(localAppData, "screentime")
}
