// Package focus defines the desktop adapters that report the focused window
// and idleness (packages/shared/src/focus.ts), and picks one at startup.
package focus

import (
	"context"
	"fmt"
	"os"
)

// Window is a focus change.
type Window struct {
	// AppID is the .desktop file ID (org.mozilla.firefox), WM_CLASS as fallback.
	AppID string
	// Title is only kept when the user opted in to title tracking.
	Title string
	PID   uint32
	// Ts is Unix ms, UTC.
	Ts int64
}

// Provider is one desktop adapter. Callbacks may run on any goroutine but
// never synchronously inside OnFocusChange or OnIdleChange: the daemon may
// hold its lock while subscribing.
type Provider interface {
	// ID is e.g. "gnome-wayland", "x11" or "none".
	ID() string
	// Available reports whether the adapter applies to this session.
	Available(ctx context.Context) bool
	OnFocusChange(cb func(Window)) (unsubscribe func())
	OnIdleChange(cb func(idle bool), thresholdMs int64) (unsubscribe func())
}

// None reports nothing: no focus changes, never idle. Selected only
// explicitly, so the RPC contract can run without a desktop session.
type None struct{}

func (None) ID() string                                   { return "none" }
func (None) Available(context.Context) bool               { return true }
func (None) OnFocusChange(func(Window)) func()            { return func() {} }
func (None) OnIdleChange(func(bool), int64) (stop func()) { return func() {} }

// Select returns the adapter named by forced (SCREENTIME_FOCUS_PROVIDER), or
// else the first available candidate.
func Select(ctx context.Context, forced string, candidates []Provider) (Provider, error) {
	byID := map[string]Provider{"none": None{}}
	for _, c := range candidates {
		byID[c.ID()] = c
	}
	if forced != "" {
		if p, ok := byID[forced]; ok {
			return p, nil
		}
		return nil, fmt.Errorf("unknown SCREENTIME_FOCUS_PROVIDER=%s", forced)
	}
	for _, c := range candidates {
		if c.Available(ctx) {
			return c, nil
		}
	}
	return nil, fmt.Errorf("no focus provider available for XDG_SESSION_TYPE=%s XDG_CURRENT_DESKTOP=%s",
		os.Getenv("XDG_SESSION_TYPE"), os.Getenv("XDG_CURRENT_DESKTOP"))
}
