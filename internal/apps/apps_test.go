package apps

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/akkki007/screentime-app/internal/store"
)

func openDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func none(string) (Entry, bool) { return Entry{}, false }

func TestParseEntryReadsOnlyTheDesktopEntryGroup(t *testing.T) {
	got := ParseEntry("[Desktop Entry]\nName=Firefox\nName[de]=Feuerfuchs\nIcon=firefox\n" +
		"[Desktop Action new-window]\nName=New Window\nIcon=other\n")
	if got != (Entry{Name: "Firefox", Icon: "firefox"}) {
		t.Errorf("ParseEntry = %+v", got)
	}
}

func TestDirLookupIgnoresPathTraversal(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "org.example.App.desktop"), []byte("[Desktop Entry]\nName=Example\n"), 0o600)
	lookup := DirLookup([]string{dir})
	if e, ok := lookup("org.example.App"); !ok || e.Name != "Example" {
		t.Errorf("lookup = %+v, %v", e, ok)
	}
	if _, ok := lookup("missing"); ok {
		t.Error("found a missing entry")
	}
	sub := DirLookup([]string{filepath.Join(dir, "sub")})
	for _, id := range []string{"../org.example.App", "a/b"} {
		if _, ok := sub(id); ok {
			t.Errorf("lookup(%q) escaped the search dir", id)
		}
	}
}

func TestWMClassIndexPrefersEarlierDirs(t *testing.T) {
	first, second := t.TempDir(), t.TempDir()
	os.WriteFile(filepath.Join(first, "org.a.desktop"), []byte("[Desktop Entry]\nStartupWMClass=Thing\n"), 0o600)
	os.WriteFile(filepath.Join(second, "org.b.desktop"), []byte("[Desktop Entry]\nStartupWMClass=thing\n"), 0o600)
	if got := WMClassIndex([]string{first, second})["thing"]; got != "org.a" {
		t.Errorf("index[thing] = %q, want org.a", got)
	}
}

func category(t *testing.T, db *sql.DB, id int64) sql.NullString {
	t.Helper()
	var name sql.NullString
	db.QueryRow("SELECT categories.name FROM apps LEFT JOIN categories ON categories.id = apps.category_id WHERE apps.id = ?", id).Scan(&name)
	return name
}

func TestEnsureCreatesWithNameIconAndCategory(t *testing.T) {
	db := openDB(t)
	id, err := Ensure(db, "org.mozilla.firefox", func(string) (Entry, bool) {
		return Entry{Name: "Firefox", Icon: "firefox"}, true
	})
	if err != nil {
		t.Fatal(err)
	}
	var name, icon string
	db.QueryRow("SELECT name, icon FROM apps WHERE id = ?", id).Scan(&name, &icon)
	if name != "Firefox" || icon != "firefox" || category(t, db, id).String != "Browsing" {
		t.Errorf("app = %s %s %v", name, icon, category(t, db, id))
	}
}

func TestEnsureNeverOverwritesAUserCategory(t *testing.T) {
	db := openDB(t)
	id, _ := Ensure(db, "org.mozilla.firefox", none)
	db.Exec("UPDATE apps SET category_id = (SELECT id FROM categories WHERE name = 'Entertainment') WHERE id = ?", id)
	again, _ := Ensure(db, "org.mozilla.firefox", none)
	if again != id || category(t, db, id).String != "Entertainment" {
		t.Errorf("id %d -> %d, category %v", id, again, category(t, db, id))
	}
}

func TestBackfillFillsGaps(t *testing.T) {
	db := openDB(t)
	unknown, _ := Ensure(db, "com.unknown.Thing", none)
	if category(t, db, unknown).Valid {
		t.Error("unknown app was categorised")
	}
	db.Exec("INSERT INTO apps (app_id) VALUES ('org.gnome.Ptyxis')")
	if err := Backfill(db, func(string) (Entry, bool) { return Entry{Name: "Terminal"}, true }); err != nil {
		t.Fatal(err)
	}
	var name, cat string
	db.QueryRow(`SELECT apps.name, categories.name FROM apps JOIN categories ON categories.id = apps.category_id
		WHERE app_id = 'org.gnome.Ptyxis'`).Scan(&name, &cat)
	if name != "Terminal" || cat != "Development" {
		t.Errorf("backfilled = %s, %s", name, cat)
	}
}
