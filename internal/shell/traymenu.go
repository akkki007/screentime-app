package shell

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/akkki007/screentime-app/internal/ipc"
)

// MenuItem is one row of the tray menu. Native tray menus can't be styled,
// but they can inform: the first rows are live status.
type MenuItem struct {
	Separator bool
	Label     string
	// Action is parsed by ParseAction; "noop" rows are display only.
	Action   string
	Disabled bool
}

// Action is a parsed tray command.
type Action struct {
	Kind    string // open, widget, pause, resume, focus, focus-stop, quit
	Minutes int
}

// TrayInfo is what the menu needs besides the tracker status.
type TrayInfo struct {
	// AppName is the display name of the app being tracked.
	AppName string
	// TodayLabel is today's total, already formatted (e.g. "4h 47m").
	TodayLabel string
	// ResumeLabel is the resume time for the paused line.
	ResumeLabel string
}

// BuildTrayMenu is the tray menu for the current daemon state. Controls are
// disabled while the daemon is down.
func BuildTrayMenu(status *ipc.TrackerStatus, connected bool, info TrayInfo) []MenuItem {
	live := connected && status != nil
	sep := MenuItem{Separator: true}
	items := []MenuItem{{Label: statusLine(status, connected, info), Action: "noop", Disabled: true}}
	if info.TodayLabel != "" && connected {
		items = append(items, MenuItem{Label: "Today  " + info.TodayLabel, Action: "noop", Disabled: true})
	}
	items = append(items, sep,
		MenuItem{Label: "Quick panel", Action: "widget"},
		MenuItem{Label: "Open dashboard", Action: "open"},
		sep)

	if status != nil && status.Paused {
		items = append(items, MenuItem{Label: "Resume tracking", Action: "resume", Disabled: !live})
	} else {
		items = append(items,
			MenuItem{Label: "Pause tracking for 15 minutes", Action: "pause:15", Disabled: !live},
			MenuItem{Label: "Pause tracking for 1 hour", Action: "pause:60", Disabled: !live})
	}

	items = append(items, sep)
	if status != nil && status.FocusMode.Active {
		items = append(items, MenuItem{Label: "Stop focus mode", Action: "focus-stop", Disabled: !live})
	} else {
		items = append(items, MenuItem{Label: "Start 25-minute focus mode", Action: "focus:25", Disabled: !live})
	}
	return append(items, sep, MenuItem{Label: "Quit Screentime", Action: "quit"})
}

func statusLine(status *ipc.TrackerStatus, connected bool, info TrayInfo) string {
	switch {
	case !connected:
		return "Daemon not running"
	case status == nil:
		return "Connecting…"
	case status.Paused && info.ResumeLabel != "":
		return "Paused until " + info.ResumeLabel
	case status.Paused:
		return "Paused"
	case status.FocusMode.Active:
		return "Focus mode on"
	case status.Idle:
		return "Idle"
	case info.AppName != "":
		return "Tracking  " + info.AppName
	}
	return "Tracking"
}

// ParseAction parses a menu action string; anything unrecognised is not ok.
func ParseAction(action string) (Action, bool) {
	switch action {
	case "open", "widget", "resume", "quit", "focus-stop":
		return Action{Kind: action}, true
	}
	kind, mins, ok := strings.Cut(action, ":")
	if !ok || (kind != "pause" && kind != "focus") {
		return Action{}, false
	}
	n, err := strconv.Atoi(mins)
	if err != nil || n <= 0 || fmt.Sprint(n) != mins {
		return Action{}, false
	}
	return Action{Kind: kind, Minutes: n}, true
}

// TrayIcon names the tray icon (an embedded asset, without extension) that
// shows the current state.
func TrayIcon(status *ipc.TrackerStatus, connected bool) string {
	switch {
	case !connected || status == nil:
		return "tray-offline"
	case status.Paused:
		return "tray-paused"
	case status.FocusMode.Active:
		return "tray-focus"
	}
	return "tray"
}

// TrayTitle is the tray tooltip: what is being tracked right now.
func TrayTitle(status *ipc.TrackerStatus, connected bool) string {
	switch {
	case !connected:
		return "Screentime: daemon not running"
	case status == nil:
		return "Screentime"
	case status.Paused:
		return "Screentime: paused"
	case status.Idle:
		return "Screentime: idle"
	}
	return "Screentime: tracking"
}

// ShortDuration formats a duration for the tray ("12m", "4h 07m").
func ShortDuration(ms int64) string {
	minutes := (ms + 30_000) / 60_000
	if h := minutes / 60; h > 0 {
		return fmt.Sprintf("%dh %02dm", h, minutes%60)
	}
	return fmt.Sprintf("%dm", minutes)
}
