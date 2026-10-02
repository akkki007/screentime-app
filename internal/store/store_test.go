package store

import (
	"database/sql"
	"os"
	"path/filepath"
	"reflect"
	"syscall"
	"testing"
)

func column(t *testing.T, db *sql.DB, query string) []string {
	t.Helper()
	rows, err := db.Query(query)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			t.Fatal(err)
		}
		out = append(out, s)
	}
	return out
}

func open(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestOpenAppliesMigrations(t *testing.T) {
	db := open(t, filepath.Join(t.TempDir(), "screentime", "t.db"))
	got := column(t, db, "SELECT name FROM sqlite_master WHERE type = 'table' ORDER BY name")
	want := []string{"apps", "categories", "limits", "sessions", "settings", "web_sessions"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("tables = %v, want %v", got, want)
	}
	names := column(t, db, "SELECT name FROM categories ORDER BY id")
	if len(names) != 8 || names[0] != "Development" || names[7] != "Other" {
		t.Errorf("categories = %v", names)
	}
	var fk int
	if err := db.QueryRow("PRAGMA foreign_keys").Scan(&fk); err != nil || fk != 1 {
		t.Errorf("foreign_keys = %d, %v", fk, err)
	}
	var mode string
	if err := db.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil || mode != "wal" {
		t.Errorf("journal_mode = %s, %v", mode, err)
	}
}

func TestOpenIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "t.db")
	first := open(t, path)
	first.Close()
	db := open(t, path)
	var version int
	if err := db.QueryRow("PRAGMA user_version").Scan(&version); err != nil {
		t.Fatal(err)
	}
	all, _ := Migrations()
	if version != len(all) {
		t.Errorf("user_version = %d, want %d", version, len(all))
	}
}

func TestMigrationsAreSequential(t *testing.T) {
	all, err := Migrations()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) < 4 {
		t.Fatalf("only %d migrations embedded", len(all))
	}
	for i, m := range all {
		if m.Version != i+1 || len(m.SQL) == 0 {
			t.Errorf("migration %d: %+v", i, m.Name)
		}
	}
}

func TestMigration0004StripsDesktopSuffixUnlessItCollides(t *testing.T) {
	path := filepath.Join(t.TempDir(), "t.db")
	db := open(t, path)
	for _, stmt := range []string{
		"PRAGMA user_version = 3",
		"INSERT INTO apps (app_id) VALUES ('org.gnome.Ptyxis.desktop'), ('org.mozilla.firefox.desktop'), ('org.mozilla.firefox'), ('plain')",
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()
	got := column(t, open(t, path), "SELECT app_id FROM apps ORDER BY app_id")
	want := []string{"org.gnome.Ptyxis", "org.mozilla.firefox", "org.mozilla.firefox.desktop", "plain"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("apps = %v, want %v", got, want)
	}
}

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
