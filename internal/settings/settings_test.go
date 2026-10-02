package settings

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/akkki007/screentime-app/internal/ipc"
	"github.com/akkki007/screentime-app/internal/store"
)

func TestDefaultsPatchingAndCorruptValueFallback(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if s, _ := Get(db); s != ipc.DefaultSettings {
		t.Errorf("defaults = %+v", s)
	}
	ten := int64(10)
	if s, err := Patch(db, ipc.SettingsPatch{IdleThresholdMinutes: &ten}); err != nil || s.IdleThresholdMinutes != 10 {
		t.Errorf("patch = %+v, %v", s, err)
	}
	for _, bad := range []string{`{"idleThresholdMinutes":0}`, `{"nope":1}`} {
		if _, err := ipc.ParseSettingsPatch(json.RawMessage(bad)); err == nil {
			t.Errorf("%s accepted", bad)
		}
	}

	// A bad stored value falls back to its default instead of failing.
	db.Exec(`INSERT OR REPLACE INTO settings (key, value) VALUES ('idleThresholdMinutes', '"oops"')`)
	db.Exec(`INSERT OR REPLACE INTO settings (key, value) VALUES ('captureTitles', '{bad json')`)
	db.Exec(`INSERT OR REPLACE INTO settings (key, value) VALUES ('unknownKey', 'true')`)
	s, err := Get(db)
	if err != nil || s.IdleThresholdMinutes != 3 || s.CaptureTitles {
		t.Errorf("fallback = %+v, %v", s, err)
	}
}
