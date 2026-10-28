//go:build !linux

package sandbox

// Detect returns nil: there is no systemd unit to adjust outside Linux.
func Detect() *Unit { return nil }

// NetworkAllowed is always true outside Linux.
func NetworkAllowed() bool { return true }
