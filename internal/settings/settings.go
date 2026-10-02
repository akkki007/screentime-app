// Package settings persists ipc.Settings in the `settings` table, one JSON
// value per key. Port of apps/daemon/src/settings-store.ts.
package settings

import (
	"database/sql"
	"encoding/json"

	"github.com/akkki007/screentime-app/internal/ipc"
)

// Get reads the settings, falling back to the default for any key that is
// missing, unparseable or out of range: a bad stored value must not stop
// the daemon from starting.
func Get(db *sql.DB) (ipc.Settings, error) {
	rows, err := db.Query("SELECT key, value FROM settings")
	if err != nil {
		return ipc.Settings{}, err
	}
	defer rows.Close()

	s := ipc.DefaultSettings
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return ipc.Settings{}, err
		}
		// Validate one key at a time with the same rules as settings.set.
		single, err := json.Marshal(map[string]json.RawMessage{key: json.RawMessage(value)})
		if err != nil || !json.Valid([]byte(value)) {
			continue
		}
		patch, err := ipc.ParseSettingsPatch(single)
		if err != nil {
			continue
		}
		Apply(&s, patch)
	}
	return s, rows.Err()
}

// Apply copies the fields set in patch onto s.
func Apply(s *ipc.Settings, p ipc.SettingsPatch) {
	setBool := func(dst *bool, v *bool) {
		if v != nil {
			*dst = *v
		}
	}
	setInt := func(dst *int64, v *int64) {
		if v != nil {
			*dst = *v
		}
	}
	setStr := func(dst *string, v *string) {
		if v != nil {
			*dst = *v
		}
	}
	setBool(&s.CaptureTitles, p.CaptureTitles)
	setInt(&s.IdleThresholdMinutes, p.IdleThresholdMinutes)
	setBool(&s.BreakRemindersEnabled, p.BreakRemindersEnabled)
	setInt(&s.BreakEveryMinutes, p.BreakEveryMinutes)
	setInt(&s.BreakLengthMinutes, p.BreakLengthMinutes)
	setBool(&s.DowntimeEnabled, p.DowntimeEnabled)
	setStr(&s.DowntimeStart, p.DowntimeStart)
	setStr(&s.DowntimeEnd, p.DowntimeEnd)
	setBool(&s.OnboardingDone, p.OnboardingDone)
}

// Patch persists a validated partial update and returns the full settings.
func Patch(db *sql.DB, p ipc.SettingsPatch) (ipc.Settings, error) {
	values := map[string]any{}
	put := func(key string, set bool, v any) {
		if set {
			values[key] = v
		}
	}
	put("captureTitles", p.CaptureTitles != nil, p.CaptureTitles)
	put("idleThresholdMinutes", p.IdleThresholdMinutes != nil, p.IdleThresholdMinutes)
	put("breakRemindersEnabled", p.BreakRemindersEnabled != nil, p.BreakRemindersEnabled)
	put("breakEveryMinutes", p.BreakEveryMinutes != nil, p.BreakEveryMinutes)
	put("breakLengthMinutes", p.BreakLengthMinutes != nil, p.BreakLengthMinutes)
	put("downtimeEnabled", p.DowntimeEnabled != nil, p.DowntimeEnabled)
	put("downtimeStart", p.DowntimeStart != nil, p.DowntimeStart)
	put("downtimeEnd", p.DowntimeEnd != nil, p.DowntimeEnd)
	put("onboardingDone", p.OnboardingDone != nil, p.OnboardingDone)

	tx, err := db.Begin()
	if err != nil {
		return ipc.Settings{}, err
	}
	defer tx.Rollback()
	for key, v := range values {
		encoded, err := json.Marshal(v)
		if err != nil {
			return ipc.Settings{}, err
		}
		if _, err := tx.Exec(
			"INSERT INTO settings (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value",
			key, string(encoded),
		); err != nil {
			return ipc.Settings{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return ipc.Settings{}, err
	}
	return Get(db)
}
