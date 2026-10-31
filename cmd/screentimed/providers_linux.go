//go:build linux

package main

import (
	"github.com/akkki007/screentime-app/internal/apps"
	"github.com/akkki007/screentime-app/internal/focus"
	"github.com/akkki007/screentime-app/internal/notify"
)

// appDirs are the directories searched for .desktop files (names and icons).
func appDirs() []string { return apps.ApplicationDirs() }

// focusCandidates are the Linux providers in priority order; the first that
// is Available wins. A new Linux provider is one more line in this list.
func focusCandidates(now func() int64, dirs []string) []focus.Provider {
	return []focus.Provider{
		&focus.Gnome{Now: now},
		&focus.KDE{Now: now},
		&focus.Wlroots{Now: now, Index: func() map[string]string { return apps.WMClassIndex(dirs) }},
		&focus.X11{Now: now, Index: func() map[string]string { return apps.WMClassIndex(dirs) }},
	}
}

func newNotifier() notify.Notifier { return &notify.DBus{} }
