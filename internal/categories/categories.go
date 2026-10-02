// Package categories holds the built-in app-to-category rules. Port of
// packages/shared/src/categories.ts; categories_test.go checks the two stay
// identical while both exist.
package categories

import "strings"

// Default is a built-in category: the rows seeded by migration 0003, plus
// the lowercase substrings that assign an app ID to it.
type Default struct {
	Name       string   `json:"name"`
	Color      string   `json:"color"`
	Productive *int     `json:"productive"`
	Match      []string `json:"match"`
}

func p(v int) *int { return &v }

// Defaults are checked in order; the first category with a matching
// substring wins.
var Defaults = []Default{
	{"Development", "#4f8cff", p(1), []string{
		"code", "vscodium", "cursor", "jetbrains", "intellij", "pycharm", "webstorm", "neovim",
		"emacs", "sublime", "ptyxis", "gnome-terminal", "console", "konsole", "alacritty", "kitty",
		"wezterm", "foot", "terminal", "xterm", "gitg", "meld", "postman", "dbeaver",
	}},
	{"Productivity", "#34c38f", p(1), []string{
		"libreoffice", "onlyoffice", "evince", "okular", "obsidian", "logseq", "notion", "gedit",
		"texteditor", "calendar", "todo", "thunderbird", "evolution", "zotero",
	}},
	{"Communication", "#a78bfa", nil, []string{
		"slack", "discord", "telegram", "signal", "element", "teams", "zoom", "whatsapp", "skype",
	}},
	{"Social", "#f472b6", p(0), []string{
		"twitter", "facebook", "instagram", "reddit", "tiktok", "mastodon", "tuba",
	}},
	{"Entertainment", "#fb923c", p(0), []string{
		"spotify", "vlc", "totem", "celluloid", "mpv", "steam", "lutris", "heroic", "netflix",
		"youtube", "rhythmbox", "clapper",
	}},
	{"Browsing", "#facc15", nil, []string{
		"firefox", "chrome", "chromium", "brave", "vivaldi", "epiphany", "opera", "librewolf", "zen",
	}},
	{"Utilities", "#94a3b8", nil, []string{
		"nautilus", "files", "dolphin", "thunar", "settings", "control-center", "software", "disks",
		"calculator", "screenshot", "gnome-system-monitor", "extensions",
	}},
	{"Other", "#64748b", nil, []string{}},
}

// DefaultFor returns the default category name for an app ID, or "" when
// nothing matches.
func DefaultFor(appID string) string {
	id := strings.ToLower(appID)
	for _, c := range Defaults {
		for _, m := range c.Match {
			if strings.Contains(id, m) {
				return c.Name
			}
		}
	}
	return ""
}
