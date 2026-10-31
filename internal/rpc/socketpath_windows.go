//go:build windows

package rpc

import "os"

// SocketPath is %LOCALAPPDATA%\screentime\daemon.sock (windowsSocketPath).
func SocketPath() string {
	home, _ := os.UserHomeDir()
	return windowsSocketPath(os.Getenv("LOCALAPPDATA"), home)
}
