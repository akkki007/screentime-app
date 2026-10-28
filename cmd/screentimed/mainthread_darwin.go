//go:build darwin

package main

import (
	"runtime"

	"github.com/akkki007/screentime-app/internal/focus"
)

// NSWorkspace posts its notifications on the main thread's run loop, so the
// main goroutine keeps the main thread (locked here, before main starts) and
// runs that loop while the daemon runs on another goroutine.
func init() { runtime.LockOSThread() }

func runOnMainThread(f func() error) error {
	result := make(chan error, 1)
	go func() {
		result <- f()
		focus.StopMainLoop()
	}()
	focus.RunMainLoop()
	return <-result
}
