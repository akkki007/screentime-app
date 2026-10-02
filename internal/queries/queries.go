// Package queries answers the read and write RPCs over the database. Port
// of apps/daemon/src/queries.ts; contract/ fixtures pin the results.
package queries

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/akkki007/screentime-app/internal/ipc"
	"github.com/akkki007/screentime-app/internal/timeutil"
)

const uncategorized = "Uncategorized"

// clipped is the overlap of a session with [:from, :to): a session that
// straddles a boundary only counts the part inside the window.
const clipped = "MAX(0, MIN(end_ts, :to) - MAX(start_ts, :from))"

func rangeArgs(from, to int64) []any {
	return []any{sql.Named("from", from), sql.Named("to", to)}
}

func scanRows(rows *sql.Rows, err error) ([]ipc.UsageRow, error) {
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ipc.UsageRow{}
	for rows.Next() {
		var r ipc.UsageRow
		if err := rows.Scan(&r.Key, &r.Ms); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// UsageSummary totals usage in [from, to) by app, category, local hour of
// day ("00".."23") or local date.
func UsageSummary(db *sql.DB, req ipc.UsageSummaryRequest) ([]ipc.UsageRow, error) {
	if req.GroupBy == "hour" || req.GroupBy == "day" {
		return bucketSessions(db, req.From, req.To, req.GroupBy)
	}
	key := "apps.app_id"
	if req.GroupBy == "category" {
		key = "COALESCE(categories.name, '" + uncategorized + "')"
	}
	return scanRows(db.Query(`SELECT `+key+` AS key, SUM(`+clipped+`) AS ms
		FROM sessions
		JOIN apps ON apps.id = sessions.app_id
		LEFT JOIN categories ON categories.id = apps.category_id
		WHERE end_ts > :from AND start_ts < :to
		GROUP BY key
		HAVING ms > 0
		ORDER BY ms DESC, key`, rangeArgs(req.From, req.To)...))
}

// bucketSessions splits sessions at local hour or day boundaries, so a
// session running 23:50-00:10 counts 10 minutes towards each day.
func bucketSessions(db *sql.DB, from, to int64, groupBy string) ([]ipc.UsageRow, error) {
	rows, err := db.Query("SELECT start_ts, end_ts FROM sessions WHERE end_ts > ? AND start_ts < ?", from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	totals := map[string]int64{}
	for rows.Next() {
		var start, end int64
		if err := rows.Scan(&start, &end); err != nil {
			return nil, err
		}
		cursor, stop := max(start, from), min(end, to)
		for cursor < stop {
			var next int64
			var key string
			if groupBy == "day" {
				next = timeutil.AddLocalDays(cursor, 1)
				key = timeutil.LocalDateKey(cursor)
			} else {
				t := time.UnixMilli(cursor).In(time.Local)
				next = time.Date(t.Year(), t.Month(), t.Day(), t.Hour()+1, 0, 0, 0, time.Local).UnixMilli()
				key = fmt.Sprintf("%02d", t.Hour())
			}
			sliceEnd := min(stop, next)
			totals[key] += sliceEnd - cursor
			cursor = sliceEnd
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := []ipc.UsageRow{}
	for k, ms := range totals {
		if ms > 0 {
			out = append(out, ipc.UsageRow{Key: k, Ms: ms})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}

// UsageWeb totals browsing time by domain in [from, to).
func UsageWeb(db *sql.DB, from, to int64) ([]ipc.UsageRow, error) {
	return scanRows(db.Query(`SELECT domain AS key, SUM(`+clipped+`) AS ms
		FROM web_sessions
		WHERE end_ts > :from AND start_ts < :to
		GROUP BY domain
		HAVING ms > 0
		ORDER BY ms DESC, key`, rangeArgs(from, to)...))
}

// Timeline lists the sessions that overlap a local date, oldest first.
func Timeline(db *sql.DB, date string) ([]ipc.Session, error) {
	from, err := timeutil.ParseLocalDate(date)
	if err != nil {
		return nil, err
	}
	rows, err := db.Query(`SELECT sessions.id, apps.app_id, sessions.title, sessions.start_ts, sessions.end_ts, sessions.source
		FROM sessions JOIN apps ON apps.id = sessions.app_id
		WHERE end_ts > ? AND start_ts < ?
		ORDER BY start_ts, sessions.id`, from, timeutil.AddLocalDays(from, 1))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ipc.Session{}
	for rows.Next() {
		var s ipc.Session
		var title sql.NullString
		if err := rows.Scan(&s.ID, &s.AppID, &title, &s.StartTs, &s.EndTs, &s.Source); err != nil {
			return nil, err
		}
		s.Title = title.String
		out = append(out, s)
	}
	return out, rows.Err()
}

// UsageFor totals the tracked ms for one app, category (by name) or domain
// within [from, to).
func UsageFor(db *sql.DB, targetType, target string, from, to int64) (int64, error) {
	var q string
	switch targetType {
	case "domain":
		q = `SELECT SUM(` + clipped + `) FROM web_sessions
			WHERE end_ts > :from AND start_ts < :to AND domain = :target`
	case "app":
		q = `SELECT SUM(` + clipped + `) FROM sessions
			JOIN apps ON apps.id = sessions.app_id
			WHERE end_ts > :from AND start_ts < :to AND apps.app_id = :target`
	default:
		q = `SELECT SUM(` + clipped + `) FROM sessions
			JOIN apps ON apps.id = sessions.app_id
			JOIN categories ON categories.id = apps.category_id
			WHERE end_ts > :from AND start_ts < :to AND categories.name = :target`
	}
	var ms sql.NullInt64
	err := db.QueryRow(q, append(rangeArgs(from, to), sql.Named("target", target))...).Scan(&ms)
	return ms.Int64, err
}

func ListApps(db *sql.DB) ([]ipc.AppInfo, error) {
	rows, err := db.Query("SELECT app_id, name, icon, category_id FROM apps ORDER BY COALESCE(name, app_id), app_id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ipc.AppInfo{}
	for rows.Next() {
		var a ipc.AppInfo
		var name, icon sql.NullString
		var cat sql.NullInt64
		if err := rows.Scan(&a.AppID, &name, &icon, &cat); err != nil {
			return nil, err
		}
		a.Name, a.Icon, a.CategoryID = strPtr(name), strPtr(icon), intPtr(cat)
		out = append(out, a)
	}
	return out, rows.Err()
}

func strPtr(v sql.NullString) *string {
	if !v.Valid {
		return nil
	}
	return &v.String
}

func intPtr(v sql.NullInt64) *int64 {
	if !v.Valid {
		return nil
	}
	return &v.Int64
}

// SetAppCategory returns false when the app or the category doesn't exist.
func SetAppCategory(db *sql.DB, appID string, categoryID *int64) (bool, error) {
	if categoryID != nil {
		var one int
		err := db.QueryRow("SELECT 1 FROM categories WHERE id = ?", *categoryID).Scan(&one)
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
	}
	res, err := db.Exec("UPDATE apps SET category_id = ? WHERE app_id = ?", categoryID, appID)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

func ListCategories(db *sql.DB) ([]ipc.Category, error) {
	rows, err := db.Query("SELECT id, name, color, productive FROM categories ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ipc.Category{}
	for rows.Next() {
		var c ipc.Category
		var color sql.NullString
		var productive sql.NullInt64
		if err := rows.Scan(&c.ID, &c.Name, &color, &productive); err != nil {
			return nil, err
		}
		c.Color, c.Productive = strPtr(color), intPtr(productive)
		out = append(out, c)
	}
	return out, rows.Err()
}

const limitColumns = "id, target_type, target, daily_ms, schedule, action"

func scanLimit(scan func(...any) error) (ipc.Limit, error) {
	var l ipc.Limit
	var id int64
	var schedule sql.NullString
	if err := scan(&id, &l.TargetType, &l.Target, &l.DailyMs, &schedule, &l.Action); err != nil {
		return ipc.Limit{}, err
	}
	l.ID, l.Schedule = &id, schedule.String
	return l, nil
}

func ListLimits(db *sql.DB) ([]ipc.Limit, error) {
	rows, err := db.Query("SELECT " + limitColumns + " FROM limits ORDER BY id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ipc.Limit{}
	for rows.Next() {
		l, err := scanLimit(rows.Scan)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// SetLimit updates the limit with l.ID, or inserts one, replacing any limit
// that already exists for the same target (and keeping its id).
func SetLimit(db *sql.DB, l ipc.Limit) (ipc.Limit, error) {
	values := []any{l.TargetType, l.Target, l.DailyMs, nullIfEmpty(l.Schedule), l.Action}
	if l.ID != nil {
		res, err := db.Exec(
			"UPDATE limits SET target_type = ?, target = ?, daily_ms = ?, schedule = ?, action = ? WHERE id = ?",
			append(values, *l.ID)...,
		)
		if err != nil {
			return ipc.Limit{}, constraintError(err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return ipc.Limit{}, fmt.Errorf("no limit with id %d", *l.ID)
		}
		return l, nil
	}
	if _, err := db.Exec(`INSERT INTO limits (target_type, target, daily_ms, schedule, action) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(target_type, target) DO UPDATE SET
			daily_ms = excluded.daily_ms, schedule = excluded.schedule, action = excluded.action`, values...); err != nil {
		return ipc.Limit{}, err
	}
	return scanLimit(db.QueryRow("SELECT "+limitColumns+" FROM limits WHERE target_type = ? AND target = ?",
		l.TargetType, l.Target).Scan)
}

// constraintError words a unique-index clash the way SQLite (and so the Bun
// daemon) does, without the driver's extra prefix and code.
func constraintError(err error) error {
	if strings.Contains(err.Error(), "UNIQUE constraint failed") {
		return errors.New("UNIQUE constraint failed: limits.target_type, limits.target")
	}
	return err
}

func DeleteLimit(db *sql.DB, id int64) (bool, error) {
	res, err := db.Exec("DELETE FROM limits WHERE id = ?", id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

type exportRow struct {
	source, target string
	start, end     int64
}

// isoTime formats like JavaScript's Date.toISOString.
func isoTime(ms int64) string {
	return time.UnixMilli(ms).UTC().Format("2006-01-02T15:04:05.000Z")
}

// ExportData exports every session (desktop and web) that overlaps
// [from, to], unclipped, as CSV or JSON. The output is byte-identical to the
// Bun daemon's.
func ExportData(db *sql.DB, format string, from, to *int64, now int64) (ipc.DataExportResponse, error) {
	lo, hi := int64(0), int64(1<<53-1) // defaults: 0 and Number.MAX_SAFE_INTEGER
	if from != nil {
		lo = *from
	}
	if to != nil {
		hi = *to
	}
	var rows []exportRow
	collect := func(source, q string) error {
		r, err := db.Query(q, lo, hi)
		if err != nil {
			return err
		}
		defer r.Close()
		for r.Next() {
			e := exportRow{source: source}
			if err := r.Scan(&e.target, &e.start, &e.end); err != nil {
				return err
			}
			rows = append(rows, e)
		}
		return r.Err()
	}
	if err := collect("desktop", `SELECT apps.app_id, sessions.start_ts, sessions.end_ts
		FROM sessions JOIN apps ON apps.id = sessions.app_id
		WHERE end_ts > ? AND start_ts < ? ORDER BY start_ts, sessions.id`); err != nil {
		return ipc.DataExportResponse{}, err
	}
	if err := collect("web", `SELECT domain, start_ts, end_ts FROM web_sessions
		WHERE end_ts > ? AND start_ts < ? ORDER BY start_ts, id`); err != nil {
		return ipc.DataExportResponse{}, err
	}
	// Stable, so on a tie desktop rows stay before web ones.
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].start < rows[j].start })

	stamp := timeutil.LocalDateKey(now)
	if format == "json" {
		type jsonRow struct {
			Source     string `json:"source"`
			Target     string `json:"target"`
			Start      string `json:"start"`
			End        string `json:"end"`
			DurationMs int64  `json:"durationMs"`
		}
		out := make([]jsonRow, 0, len(rows))
		for _, r := range rows {
			out = append(out, jsonRow{r.source, r.target, isoTime(r.start), isoTime(r.end), r.end - r.start})
		}
		content, err := jsStringify(out)
		if err != nil {
			return ipc.DataExportResponse{}, err
		}
		return ipc.DataExportResponse{Filename: "screentime-" + stamp + ".json", Content: content}, nil
	}

	var b strings.Builder
	b.WriteString("source,target,start,end,duration_ms\n")
	for _, r := range rows {
		b.WriteString(strings.Join([]string{
			r.source, csvCell(r.target), isoTime(r.start), isoTime(r.end), strconv.FormatInt(r.end-r.start, 10),
		}, ","))
		b.WriteString("\n")
	}
	return ipc.DataExportResponse{Filename: "screentime-" + stamp + ".csv", Content: b.String()}, nil
}

// jsStringify matches JSON.stringify(v, null, 2): two-space indent, no HTML
// escaping, no trailing newline.
func jsStringify(v any) (string, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return "", err
	}
	return strings.TrimSuffix(buf.String(), "\n"), nil
}

var (
	formulaStart = regexp.MustCompile(`^[=+\-@\t\r]`)
	needsQuotes  = regexp.MustCompile(`[",\n]`)
)

// csvCell quotes when needed and defuses spreadsheet formula injection from
// app and domain names.
func csvCell(value string) string {
	if formulaStart.MatchString(value) {
		value = "'" + value
	}
	if needsQuotes.MatchString(value) {
		return `"` + strings.ReplaceAll(value, `"`, `""`) + `"`
	}
	return value
}

// WipeData deletes tracked usage; with everything, also settings, limits
// and apps. The space is reclaimed so deleted history isn't left readable
// in free pages.
func WipeData(db *sql.DB, everything bool) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	stmts := []string{"DELETE FROM sessions", "DELETE FROM web_sessions"}
	if everything {
		stmts = append(stmts, "DELETE FROM limits", "DELETE FROM settings", "DELETE FROM apps")
	}
	for _, s := range stmts {
		if _, err := tx.Exec(s); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if _, err := db.Exec("VACUUM"); err != nil {
		return err
	}
	_, err = db.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
	return err
}
