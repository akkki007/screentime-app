package apps

import (
	"os"
	"path/filepath"
	"strings"
)

// Entry is what a .desktop file contributes: unlocalised Name and Icon, and
// StartupWMClass for resolving X11 windows.
type Entry struct {
	Name, Icon, StartupWMClass string
}

// Lookup finds the desktop entry for an app ID; ok is false if none exists.
type Lookup func(appID string) (Entry, bool)

// ApplicationDirs are searched for <appId>.desktop, per the XDG base
// directory spec plus Flatpak and Snap exports. Earlier directories win.
func ApplicationDirs() []string {
	home := os.Getenv("HOME")
	dataHome := os.Getenv("XDG_DATA_HOME")
	if dataHome == "" {
		dataHome = filepath.Join(home, ".local", "share")
	}
	dataDirs := os.Getenv("XDG_DATA_DIRS")
	if dataDirs == "" {
		dataDirs = "/usr/local/share:/usr/share"
	}
	dirs := []string{filepath.Join(dataHome, "applications")}
	for _, d := range strings.Split(dataDirs, ":") {
		if d != "" {
			dirs = append(dirs, filepath.Join(d, "applications"))
		}
	}
	return append(dirs,
		filepath.Join(home, ".local/share/flatpak/exports/share/applications"),
		"/var/lib/flatpak/exports/share/applications",
		"/var/lib/snapd/desktop/applications",
	)
}

// ParseEntry reads the first Name, Icon and StartupWMClass from the
// [Desktop Entry] group.
func ParseEntry(text string) Entry {
	var e Entry
	var haveName, haveIcon, haveClass, inEntry bool
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "[") {
			inEntry = line == "[Desktop Entry]"
			continue
		}
		if !inEntry || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		value = strings.TrimSpace(value)
		switch {
		case key == "Name" && !haveName:
			e.Name, haveName = value, true
		case key == "Icon" && !haveIcon:
			e.Icon, haveIcon = value, true
		case key == "StartupWMClass" && !haveClass:
			e.StartupWMClass, haveClass = value, true
		}
	}
	return e
}

// DirLookup returns a Lookup over dirs.
func DirLookup(dirs []string) Lookup {
	return func(appID string) (Entry, bool) {
		// App IDs come from the compositor; never let one climb out of the dirs.
		if appID == "" || strings.Contains(appID, "/") || strings.Contains(appID, "..") {
			return Entry{}, false
		}
		for _, dir := range dirs {
			text, err := os.ReadFile(filepath.Join(dir, appID+".desktop"))
			if err == nil {
				return ParseEntry(string(text)), true
			}
		}
		return Entry{}, false
	}
}

// WMClassIndex maps lowercased StartupWMClass to the desktop-entry ID, so an
// X11 window known only by WM_CLASS resolves to the ID Wayland reports.
func WMClassIndex(dirs []string) map[string]string {
	index := map[string]string{}
	for _, dir := range dirs {
		files, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, f := range files {
			name := f.Name()
			if !strings.HasSuffix(name, ".desktop") {
				continue
			}
			text, err := os.ReadFile(filepath.Join(dir, name))
			if err != nil {
				continue
			}
			key := strings.ToLower(ParseEntry(string(text)).StartupWMClass)
			if _, taken := index[key]; key != "" && !taken {
				index[key] = strings.TrimSuffix(name, ".desktop")
			}
		}
	}
	return index
}
