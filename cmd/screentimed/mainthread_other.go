//go:build !darwin

package main

// runOnMainThread: only macOS needs the main thread (mainthread_darwin.go).
func runOnMainThread(f func() error) error { return f() }
