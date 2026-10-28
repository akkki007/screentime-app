package store

import (
	"path/filepath"
	"testing"
)

func TestDataPath(t *testing.T) {
	env := func(m map[string]string) func(string) string { return func(k string) string { return m[k] } }
	join := filepath.Join
	cases := []struct {
		name, goos string
		env        map[string]string
		home, want string
	}{
		{"linux XDG_DATA_HOME", "linux", map[string]string{"XDG_DATA_HOME": "/xdg", "HOME": "/home/ada"}, "/home/ada",
			join("/xdg", "screentime", "screentime.db")},
		{"linux default", "linux", map[string]string{"HOME": "/home/ada"}, "/home/ada",
			join("/home/ada", ".local", "share", "screentime", "screentime.db")},
		{"macOS ignores XDG", "darwin", map[string]string{"XDG_DATA_HOME": "/xdg"}, "/Users/ada",
			join("/Users/ada", "Library", "Application Support", "screentime", "screentime.db")},
		{"windows LOCALAPPDATA", "windows", map[string]string{"LOCALAPPDATA": "L", "XDG_DATA_HOME": "/xdg"}, "H",
			join("L", "screentime", "screentime.db")},
		{"windows fallback", "windows", nil, "H",
			join("H", "AppData", "Local", "screentime", "screentime.db")},
	}
	for _, c := range cases {
		if got := dataPath(c.goos, env(c.env), c.home); got != c.want {
			t.Errorf("%s: dataPath = %s, want %s", c.name, got, c.want)
		}
	}
}
