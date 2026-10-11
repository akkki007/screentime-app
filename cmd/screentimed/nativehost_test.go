package main

import "testing"

func TestIsNativeHost(t *testing.T) {
	cases := []struct {
		args []string
		want bool
	}{
		{[]string{"screentimed"}, false},
		{[]string{"/usr/bin/screentimed", "native-host"}, true},
		{[]string{"/usr/lib/screentime/screentime-native-host"}, true},
		{[]string{"/usr/lib/screentime/screentime-native-host", "/path/to/manifest.json", "screentime@akkki007.github.io"}, true},
		{[]string{"/usr/lib/screentime/screentime-native-host", "chrome-extension://abc/"}, true},
		{[]string{"screentimed", "other"}, false},
	}
	for _, c := range cases {
		if got := isNativeHost(c.args); got != c.want {
			t.Errorf("isNativeHost(%q) = %v, want %v", c.args, got, c.want)
		}
	}
}
