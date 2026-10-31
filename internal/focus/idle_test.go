package focus

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

// pollIdle reports threshold crossings only, and survives query errors.
func TestPollIdleReportsCrossingsOnly(t *testing.T) {
	readings := []int64{0, 100, 250, 400, -1, 900, 50, 20}
	next := 0
	exhausted := make(chan struct{})
	// query runs on the polling goroutine only.
	query := func() (int64, error) {
		if next >= len(readings) {
			return 0, nil
		}
		v := readings[next]
		next++
		if next == len(readings) {
			close(exhausted)
		}
		if v < 0 {
			return 0, errors.New("server went away for a moment")
		}
		return v, nil
	}
	got := make(chan bool, 8)
	done := make(chan struct{})
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		pollIdle("test", done, time.Millisecond, 300, query, func(idle bool) { got <- idle })
	}()
	select {
	case <-exhausted:
	case <-time.After(5 * time.Second):
		t.Fatal("readings were not consumed")
	}
	close(done)
	<-stopped
	var seen []bool
	for len(got) > 0 {
		seen = append(seen, <-got)
	}
	if want := []bool{true, false}; !reflect.DeepEqual(seen, want) {
		t.Errorf("reports = %v, want %v", seen, want)
	}
}

func TestIdlePollEvery(t *testing.T) {
	cases := map[int64]time.Duration{
		0:         time.Second,
		2_000:     time.Second,
		12_000:    3 * time.Second,
		20_000:    5 * time.Second,
		300_000:   5 * time.Second,
		3_600_000: 5 * time.Second,
	}
	for ms, want := range cases {
		if got := idlePollEvery(ms); got != want {
			t.Errorf("idlePollEvery(%d) = %v, want %v", ms, got, want)
		}
	}
}

func TestAppIDFromExe(t *testing.T) {
	cases := map[string]string{
		`C:\Program Files\Google\Chrome\Application\chrome.exe`: "chrome",
		`C:\Windows\System32\NOTEPAD.EXE`:                       "notepad",
		`C:\Apps\Code.Insiders.exe`:                             "code.insiders",
		`D:\tools\no-extension`:                                 "no-extension",
		`/mnt/c/apps/Slack.exe`:                                 "slack",
		`.exe`:                                                  ".exe",
		`C:\dir\`:                                               "unknown",
		``:                                                      "unknown",
	}
	for in, want := range cases {
		if got := appIDFromExe(in); got != want {
			t.Errorf("appIDFromExe(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAppIDFromBundle(t *testing.T) {
	if got := appIDFromBundle("com.apple.Safari", "Safari"); got != "com.apple.Safari" {
		t.Errorf("bundle ID not preferred: %q", got)
	}
	if got := appIDFromBundle("", "Some Helper"); got != "Some Helper" {
		t.Errorf("name fallback: %q", got)
	}
	if got := appIDFromBundle("", ""); got != "unknown" {
		t.Errorf("last resort: %q", got)
	}
}

func TestIdleSinceSurvivesTickWrap(t *testing.T) {
	if got := idleSince(10_000, 4_000); got != 6_000 {
		t.Errorf("plain: %d", got)
	}
	// GetTickCount64 is past 2^32 ms (49.7 days); GetLastInputInfo's tick wrapped.
	const wrap = 1 << 32
	if got := idleSince(wrap+500, wrap-1_500); got != 2_000 {
		t.Errorf("across the wrap: %d", got)
	}
}
