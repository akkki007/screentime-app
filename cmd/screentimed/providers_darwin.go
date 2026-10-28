//go:build darwin

package main

import (
	"github.com/akkki007/screentime-app/internal/focus"
	"github.com/akkki007/screentime-app/internal/notify"
)

// There are no .desktop files on macOS: apps are named by their bundle
// identifier until a display-name source (Info.plist) is added.
func appDirs() []string { return nil }

func focusCandidates(now func() int64, _ []string) []focus.Provider {
	return []focus.Provider{&focus.Darwin{Now: now}}
}

// No user notifications yet (ADR 13): limits are enforced, but the nudge is
// only logged.
func newNotifier() notify.Notifier { return &notify.Log{} }
