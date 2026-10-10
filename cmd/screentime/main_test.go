//go:build gtk3

package main

import (
	"reflect"
	"testing"
)

func TestWithoutEnv(t *testing.T) {
	env := []string{"A=1", "SCREENTIME_PARENT_PIPE=1", "SCREENTIME_PARENT_PIPE_X=2", "B="}
	got := withoutEnv(env, "SCREENTIME_PARENT_PIPE")
	want := []string{"A=1", "SCREENTIME_PARENT_PIPE_X=2", "B="}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("withoutEnv = %v, want %v", got, want)
	}
	if len(env) != 4 || env[1] != "SCREENTIME_PARENT_PIPE=1" {
		t.Fatalf("withoutEnv modified its input: %v", env)
	}
}

func TestFallbackName(t *testing.T) {
	for in, want := range map[string]string{
		"org.mozilla.firefox":   "Firefox",
		"org.gnome.Ptyxis":      "Ptyxis",
		"code_code":             "Code",
		"":                      "",
		"io.github.foo.Bar_baz": "Bar",
	} {
		if got := fallbackName(in); got != want {
			t.Errorf("fallbackName(%q) = %q, want %q", in, got, want)
		}
	}
}
