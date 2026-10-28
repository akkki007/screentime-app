//go:build darwin && !cgo

package focus

import "context"

// Darwin without cgo: the macOS provider needs Objective-C (darwin.go), so a
// CGO_ENABLED=0 build compiles, which keeps `go vet` usable without Xcode,
// but reports itself unavailable.
type Darwin struct {
	Now func() int64
}

func (d *Darwin) ID() string                                   { return "macos" }
func (d *Darwin) Available(context.Context) bool               { return false }
func (d *Darwin) OnFocusChange(func(Window)) func()            { return func() {} }
func (d *Darwin) OnIdleChange(func(bool), int64) (stop func()) { return func() {} }

// RunMainLoop blocks until StopMainLoop, as the cgo version does.
func RunMainLoop() { <-stopMainLoop }

// StopMainLoop makes RunMainLoop return.
func StopMainLoop() { close(stopMainLoop) }

var stopMainLoop = make(chan struct{})
