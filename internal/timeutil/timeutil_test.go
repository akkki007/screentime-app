package timeutil

import (
	"testing"
	"time"
)

// withZone runs the test with time.Local set to an IANA zone.
func withZone(t *testing.T, name string) {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatal(err)
	}
	prev := time.Local
	time.Local = loc
	t.Cleanup(func() { time.Local = prev })
}

func ms(t *testing.T, s string) int64 {
	t.Helper()
	v, err := time.ParseInLocation("2006-01-02 15:04", s, time.Local)
	if err != nil {
		t.Fatal(err)
	}
	return v.UnixMilli()
}

func TestDayBoundariesInAHalfHourZone(t *testing.T) {
	withZone(t, "Asia/Kolkata")
	ts := ms(t, "2026-09-30 00:15")
	if got, want := StartOfLocalDay(ts), ms(t, "2026-09-30 00:00"); got != want {
		t.Errorf("StartOfLocalDay = %d, want %d", got, want)
	}
	if got := LocalDateKey(ts); got != "2026-09-30" {
		t.Errorf("LocalDateKey = %s (the UTC date is still the 29th)", got)
	}
	if got, want := AddLocalDays(ts, -1), ms(t, "2026-09-29 00:00"); got != want {
		t.Errorf("AddLocalDays(-1) = %d, want %d", got, want)
	}
	if got := MinutesIntoDay(ts); got != 15 {
		t.Errorf("MinutesIntoDay = %d, want 15", got)
	}
}

func TestAddLocalDaysAcrossDST(t *testing.T) {
	withZone(t, "America/New_York")
	// 2026-03-08 is 23 hours long in New York.
	start := ms(t, "2026-03-08 12:00")
	got := AddLocalDays(start, 1)
	if want := ms(t, "2026-03-09 00:00"); got != want {
		t.Errorf("AddLocalDays = %d, want %d", got, want)
	}
	if day := AddLocalDays(start, 1) - StartOfLocalDay(start); day != 23*3600*1000 {
		t.Errorf("DST day length = %d ms, want 23 h", day)
	}
}

func TestParseLocalDate(t *testing.T) {
	withZone(t, "Asia/Kolkata")
	cases := []struct {
		in   string
		want string // local, or "" for an error
	}{
		{"2026-09-30", "2026-09-30 00:00"},
		{"2026-02-31", "2026-03-03 00:00"}, // normalised, like JavaScript's Date
		{"2026-13-01", "2027-01-01 00:00"},
		{"not-a-date", ""},
		{"2026-9-30", ""},
		{" 2026-09-30", ""},
	}
	for _, c := range cases {
		got, err := ParseLocalDate(c.in)
		if c.want == "" {
			if err == nil || err.Error() != "invalid date: "+c.in {
				t.Errorf("ParseLocalDate(%q) err = %v, want invalid date", c.in, err)
			}
			continue
		}
		if err != nil || got != ms(t, c.want) {
			t.Errorf("ParseLocalDate(%q) = %d, %v; want %s", c.in, got, err, c.want)
		}
	}
}

func TestParseClockAndWindow(t *testing.T) {
	if got := ParseClock("22:30"); got != 1350 {
		t.Errorf("ParseClock = %d", got)
	}
	night := [2]int{ParseClock("22:00"), ParseClock("07:00")}
	day := [2]int{ParseClock("09:00"), ParseClock("18:00")}
	cases := []struct {
		minute int
		window [2]int
		want   bool
	}{
		{ParseClock("23:00"), night, true},
		{ParseClock("06:59"), night, true},
		{ParseClock("07:00"), night, false},
		{ParseClock("12:00"), night, false},
		{ParseClock("09:00"), day, true},
		{ParseClock("18:00"), day, false},
		{ParseClock("10:00"), [2]int{600, 600}, false},
	}
	for _, c := range cases {
		if got := InDailyWindow(c.minute, c.window[0], c.window[1]); got != c.want {
			t.Errorf("InDailyWindow(%d, %v) = %v", c.minute, c.window, got)
		}
	}
}
