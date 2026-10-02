// Package timeutil holds the local-time helpers. Timestamps are Unix ms in
// UTC everywhere; only the boundaries of "a day" and "an hour" depend on the
// user's time zone (time.Local, which honours $TZ).
//
// Ported from apps/daemon/src/time.ts. Like JavaScript's Date, time.Date
// normalises out-of-range fields (Feb 31 becomes Mar 3), so both agree.
package timeutil

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

func local(ts int64) time.Time { return time.UnixMilli(ts).In(time.Local) }

// StartOfLocalDay is local midnight at the start of the day containing ts.
func StartOfLocalDay(ts int64) int64 {
	t := local(ts)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.Local).UnixMilli()
}

// AddLocalDays is the start of the local day `days` after the one containing
// ts. Unlike adding 24 h, it is correct across DST changes.
func AddLocalDays(ts int64, days int) int64 {
	t := local(ts)
	return time.Date(t.Year(), t.Month(), t.Day()+days, 0, 0, 0, 0, time.Local).UnixMilli()
}

// LocalDateKey formats the local date of ts as YYYY-MM-DD.
func LocalDateKey(ts int64) string {
	return local(ts).Format("2006-01-02")
}

var dateRe = regexp.MustCompile(`^(\d{4})-(\d{2})-(\d{2})$`)

// ParseLocalDate parses YYYY-MM-DD as local midnight.
func ParseLocalDate(date string) (int64, error) {
	m := dateRe.FindStringSubmatch(date)
	if m == nil {
		return 0, fmt.Errorf("invalid date: %s", date)
	}
	y, _ := strconv.Atoi(m[1])
	mo, _ := strconv.Atoi(m[2])
	d, _ := strconv.Atoi(m[3])
	return time.Date(y, time.Month(mo), d, 0, 0, 0, 0, time.Local).UnixMilli(), nil
}

// MinutesIntoDay is the number of minutes since local midnight at ts.
func MinutesIntoDay(ts int64) int {
	t := local(ts)
	return t.Hour()*60 + t.Minute()
}

// ParseClock parses HH:MM into minutes since midnight. Callers validate the
// format first; a missing or unparseable part counts as 0, as in time.ts.
func ParseClock(clock string) int {
	parts := strings.SplitN(clock, ":", 2)
	h, _ := strconv.Atoi(parts[0])
	m := 0
	if len(parts) > 1 {
		m, _ = strconv.Atoi(parts[1])
	}
	return h*60 + m
}

// InDailyWindow reports whether minute falls in [start, end), where the
// window may wrap past midnight (22:00-07:00). An empty window (start ==
// end) is never active.
func InDailyWindow(minute, start, end int) bool {
	if start == end {
		return false
	}
	if start < end {
		return minute >= start && minute < end
	}
	return minute >= start || minute < end
}
