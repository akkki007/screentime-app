//go:build windows

package main

import (
	"github.com/akkki007/screentime-app/internal/focus"
	"github.com/akkki007/screentime-app/internal/notify"
)

// There are no .desktop files on Windows: apps are named by their exe name
// until a display-name source (version info, Start menu) is added.
func appDirs() []string { return nil }

func focusCandidates(now func() int64, _ []string) []focus.Provider {
	return []focus.Provider{&focus.Windows{Now: now}}
}

// No toast notifications yet (ADR 13): limits are enforced, but the nudge is
// only logged.
func newNotifier() notify.Notifier { return &notify.Log{} }
