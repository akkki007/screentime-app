package main

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/akkki007/screentime-app/internal/store"
)

func seed(t *testing.T, sessions [][2]int64) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "t.db")
	db, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`INSERT INTO apps (id, app_id, name) VALUES (1, 'org.test.App', 'App')`); err != nil {
		t.Fatal(err)
	}
	for _, s := range sessions {
		if _, err := db.Exec(`INSERT INTO sessions (app_id, start_ts, end_ts, source) VALUES (1, ?, ?, 'desktop')`, s[0], s[1]); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func TestVerifyAcceptsCleanSessions(t *testing.T) {
	path := seed(t, [][2]int64{{1000, 5000}, {5000, 9000}, {20000, 30000}})
	if !verify(path, time.Time{}, time.Hour) {
		t.Fatal("clean sessions reported as faulty")
	}
}

func TestVerifyFlagsOverlap(t *testing.T) {
	path := seed(t, [][2]int64{{1000, 5000}, {4000, 9000}})
	if verify(path, time.Time{}, time.Hour) {
		t.Fatal("overlap (double counting) not flagged")
	}
}

func TestVerifyFlagsInvertedSession(t *testing.T) {
	path := seed(t, [][2]int64{{5000, 1000}})
	if verify(path, time.Time{}, time.Hour) {
		t.Fatal("end before start not flagged")
	}
}
