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
