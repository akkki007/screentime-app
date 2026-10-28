package categories

import (
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

// The TypeScript rules (packages/shared/src/categories.ts) are exported to
// contract/schema/categories.json; both daemons must classify apps alike.
func TestMatchesTypeScriptRules(t *testing.T) {
	raw, err := os.ReadFile("../../contract/schema/categories.json")
	if err != nil {
		t.Fatal(err)
	}
	var ts []Default
	if err := json.Unmarshal(raw, &ts); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(ts, Defaults) {
		t.Errorf("categories differ from categories.ts; port the change to categories.go")
	}
}

func TestDefaultFor(t *testing.T) {
	cases := map[string]string{
		"code":                "Development",
		"org.gnome.Ptyxis":    "Development",
		"org.mozilla.firefox": "Browsing",
		"com.spotify.Client":  "Entertainment",
		"org.gnome.Nautilus":  "Utilities",
		"com.example.Unknown": "",
		"org.gnome.Console":   "Development",
	}
	for id, want := range cases {
		if got := DefaultFor(id); got != want {
			t.Errorf("DefaultFor(%q) = %q, want %q", id, got, want)
		}
	}
}

func TestFromDesktop(t *testing.T) {
	cases := []struct {
		cats []string
		want string
	}{
		{[]string{"Utility", "TextEditor", "Development", "IDE"}, "Development"},
		{[]string{"AudioVideo", "Video", "Player"}, "Entertainment"},
		{[]string{"Office", "WordProcessor"}, "Productivity"},
		{[]string{"Office", "Spreadsheet"}, "Productivity"},
		{[]string{"GNOME", "GTK", "Office", "Viewer", "Graphics"}, "Productivity"},
		{[]string{"Network", "FileTransfer", "P2P"}, "Utilities"},
		{[]string{"Network"}, "Utilities"},
		{[]string{"System", "Settings"}, "Utilities"},
		{[]string{"System", "Utility", "PackageManager"}, "Utilities"},
		{[]string{"Graphics", "Photography"}, "Productivity"},
		{[]string{"GTK", "GNOME", "Utility"}, "Utilities"},
		{[]string{"Utility", "Development"}, "Development"},
		{[]string{"Network", "WebBrowser"}, "Browsing"},
		{[]string{"Network", "Chat", "InstantMessaging"}, "Communication"},
		{[]string{"Network", "Email"}, "Productivity"},
		{[]string{"Game", "Utility"}, "Entertainment"},
		{[]string{"Utility", "TerminalEmulator", "System"}, "Development"},
		{nil, ""},
		{[]string{}, ""},
		{[]string{"GNOME", "GTK", "KDE", "Qt", "X-GNOME-Utilities"}, ""},
		{[]string{"Bogus", "Whatever"}, ""},
	}
	for _, c := range cases {
		if got := FromDesktop(c.cats); got != c.want {
			t.Errorf("FromDesktop(%v) = %q, want %q", c.cats, got, c.want)
		}
	}
}
