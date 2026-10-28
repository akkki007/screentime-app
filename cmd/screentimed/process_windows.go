//go:build windows

package main

// restrictUmask does nothing: Windows has no umask. Files take the ACL of
// %LOCALAPPDATA%\screentime, which is per-user (S1; see internal/rpc/perm_windows.go).
func restrictUmask() {}
