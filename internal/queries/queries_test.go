package queries

import (
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/akkki007/screentime-app/internal/apps"
	"github.com/akkki007/screentime-app/internal/ipc"
	"github.com/akkki007/screentime-app/internal/settings"
	"github.com/akkki007/screentime-app/internal/store"
)

// Most query behaviour is pinned by the contract fixtures (contract/); these
// cover what they can't reach.

func openDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func addSession(t *testing.T, db *sql.DB, appID string, start, end time.Time) {
	t.Helper()
	id, err := apps.Ensure(db, appID, func(string) (apps.Entry, bool) { return apps.Entry{}, false })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO sessions (app_id, start_ts, end_ts, source) VALUES (?, ?, ?, 'desktop')",
		id, start.UnixMilli(), end.UnixMilli()); err != nil {
		t.Fatal(err)
	}
}

func day(h int) time.Time { return time.Date(2026, 1, 1, h, 0, 0, 0, time.Local) }

func TestUpdatingAMissingLimitFails(t *testing.T) {
	db := openDB(t)
	id := int64(42)
	_, err := SetLimit(db, ipc.Limit{ID: &id, TargetType: "app", Target: "a", DailyMs: 60_000, Action: "notify"})
	if err == nil || err.Error() != "no limit with id 42" {
		t.Errorf("err = %v", err)
	}
}

func TestUpdatingALimitOntoAnotherTargetReportsTheClash(t *testing.T) {
	db := openDB(t)
	a, _ := SetLimit(db, ipc.Limit{TargetType: "app", Target: "a", DailyMs: 60_000, Action: "notify"})
	SetLimit(db, ipc.Limit{TargetType: "app", Target: "b", DailyMs: 60_000, Action: "notify"})
	_, err := SetLimit(db, ipc.Limit{ID: a.ID, TargetType: "app", Target: "b", DailyMs: 60_000, Action: "notify"})
	if err == nil || err.Error() != "UNIQUE constraint failed: limits.target_type, limits.target" {
		t.Errorf("err = %v", err)
	}
}

func TestCSVQuotesAndDefusesFormulas(t *testing.T) {
	db := openDB(t)
	addSession(t, db, "=cmd|calc", day(9), day(10))
	addSession(t, db, "a,b", day(10), day(11))
	addSession(t, db, `say "hi"`, day(11), day(12))
	res, err := ExportData(db, "csv", nil, nil, time.Date(2026, 3, 4, 12, 0, 0, 0, time.Local).UnixMilli())
	if err != nil {
		t.Fatal(err)
	}
	if res.Filename != "screentime-2026-03-04.csv" {
		t.Errorf("filename = %s", res.Filename)
	}
	for _, want := range []string{"desktop,'=cmd|calc,", `desktop,"a,b",`, `desktop,"say ""hi""",`} {
		if !strings.Contains(res.Content, want) {
			t.Errorf("CSV lacks %q:\n%s", want, res.Content)
		}
	}
}

func TestJSONExportMatchesJavaScript(t *testing.T) {
	db := openDB(t)
	empty, _ := ExportData(db, "json", nil, nil, 0)
	if empty.Content != "[]" {
		t.Errorf("empty export = %q, want []", empty.Content)
	}
	addSession(t, db, "a<b>&c", day(9), day(10))
	res, _ := ExportData(db, "json", nil, nil, 0)
	// JSON.stringify doesn't escape HTML characters.
	if !strings.Contains(res.Content, `"target": "a<b>&c"`) {
		t.Errorf("JSON escaped HTML:\n%s", res.Content)
	}
	var rows []map[string]any
	if err := json.Unmarshal([]byte(res.Content), &rows); err != nil || rows[0]["durationMs"] != 3600000.0 {
		t.Errorf("rows = %v, %v", rows, err)
	}
}

func TestWipeKeepsSettingsAndLimitsUnlessAsked(t *testing.T) {
	db := openDB(t)
	addSession(t, db, "a", day(9), day(10))
	SetLimit(db, ipc.Limit{TargetType: "app", Target: "a", DailyMs: 60_000, Action: "notify"})
	on := true
	settings.Patch(db, ipc.SettingsPatch{CaptureTitles: &on})

	count := func(table string) (n int) {
		db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n)
		return n
	}
	if err := WipeData(db, false); err != nil {
		t.Fatal(err)
	}
	s, _ := settings.Get(db)
	if count("sessions") != 0 || count("limits") != 1 || !s.CaptureTitles {
		t.Errorf("after wipe: sessions %d, limits %d, captureTitles %v", count("sessions"), count("limits"), s.CaptureTitles)
	}
	WipeData(db, true)
	s, _ = settings.Get(db)
	if count("limits") != 0 || count("apps") != 0 || s.CaptureTitles {
		t.Errorf("after wipe everything: limits %d, apps %d, captureTitles %v", count("limits"), count("apps"), s.CaptureTitles)
	}
}
