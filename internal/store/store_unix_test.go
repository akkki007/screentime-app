//go:build unix

package store

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func mode(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode().Perm()
}

// S1 / issue #4: nothing is readable by other users, even with a permissive
// umask and even when the directory and database already existed.
func TestDataIsPrivate(t *testing.T) {
	old := syscall.Umask(0o022)
	t.Cleanup(func() { syscall.Umask(old) })

	t.Run("fresh", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "screentime")
		db := open(t, filepath.Join(dir, "screentime.db"))
		if _, err := db.Exec("INSERT INTO web_sessions (domain, start_ts, end_ts) VALUES ('x', 1, 2)"); err != nil {
			t.Fatal(err)
		}
		if m := mode(t, dir); m != 0o700 {
			t.Errorf("dir mode = %o, want 700", m)
		}
		for _, suffix := range []string{"", "-wal", "-shm"} {
			if m := mode(t, filepath.Join(dir, "screentime.db"+suffix)); m != 0o600 {
				t.Errorf("screentime.db%s mode = %o, want 600", suffix, m)
			}
		}
	})

	t.Run("existing", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "screentime")
		path := filepath.Join(dir, "screentime.db")
		if err := os.MkdirAll(dir, 0o777); err != nil {
			t.Fatal(err)
		}
		os.Chmod(dir, 0o777)
		if err := os.WriteFile(path, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		open(t, path)
		if m := mode(t, dir); m != 0o700 {
			t.Errorf("dir mode = %o, want 700", m)
		}
		if m := mode(t, path); m != 0o600 {
			t.Errorf("db mode = %o, want 600", m)
		}
	})
}
