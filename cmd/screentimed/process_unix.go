//go:build unix

package main

import "syscall"

// restrictUmask makes everything the daemon creates private to the user (S1).
func restrictUmask() { syscall.Umask(0o077) }
