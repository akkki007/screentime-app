package tracker

import (
	"reflect"
	"testing"
)

type webRow struct {
	domain         string
	startTs, endTs int64
}

type webHarness struct {
	t     *testing.T
	web   *WebTracker
	clock int64
}

func setupWeb(t *testing.T) *webHarness {
	h := &webHarness{t: t, clock: 1_000_000}
	h.web = NewWebTracker(openDB(t), func() int64 { return h.clock })
	return h
}

func (h *webHarness) report(domain string, tracking bool) { h.web.Handle(&domain, true, tracking) }

func (h *webHarness) rows() []webRow {
	h.t.Helper()
	rs, err := h.web.db.Query("SELECT domain, start_ts, end_ts FROM web_sessions ORDER BY id")
	if err != nil {
		h.t.Fatal(err)
	}
	defer rs.Close()
	var out []webRow
	for rs.Next() {
		var r webRow
		if err := rs.Scan(&r.domain, &r.startTs, &r.endTs); err != nil {
			h.t.Fatal(err)
		}
		out = append(out, r)
	}
	if err := rs.Err(); err != nil {
		h.t.Fatal(err)
	}
	return out
}

func (h *webHarness) wantRows(want []webRow) {
	h.t.Helper()
	if got := h.rows(); !reflect.DeepEqual(got, want) {
		h.t.Errorf("rows = %+v, want %+v", got, want)
	}
}

func (h *webHarness) wantCount(n int) []webRow {
	h.t.Helper()
	rows := h.rows()
	if len(rows) != n {
		h.t.Fatalf("got %d rows %+v, want %d", len(rows), rows, n)
	}
	return rows
}

func TestWebTracker(t *testing.T) {
	t.Run("opens a session for the reported domain and extends it on heartbeats", func(t *testing.T) {
		h := setupWeb(t)
		h.report("a.com", true)
		h.clock += 5_000
		h.web.Sync(true)
		h.wantRows([]webRow{{"a.com", 1_000_000, 1_005_000}})
	})

	t.Run("switching domains closes one session and opens the next", func(t *testing.T) {
		h := setupWeb(t)
		h.report("a.com", true)
		h.clock += 4_000
		h.report("b.com", true)
		h.wantRows([]webRow{
			{"a.com", 1_000_000, 1_004_000},
			{"b.com", 1_004_000, 1_004_000},
		})
	})

	t.Run("does not count time while no browser is focused, and resumes on return", func(t *testing.T) {
		h := setupWeb(t)
		h.report("a.com", true)
		h.clock += 5_000
		h.web.Sync(true)
		h.clock += 60_000
		h.web.Sync(false) // user went to the terminal
		h.clock += 60_000
		h.web.Sync(false)
		if got := h.wantCount(1)[0].endTs; got != 1_005_000 {
			t.Errorf("end_ts = %d, want 1005000", got)
		}

		h.web.Sync(true) // back in the browser, same tab: no new extension message
		if r := h.wantCount(2)[1]; r.domain != "a.com" || r.startTs != h.clock {
			t.Errorf("row = %+v, want a.com starting %d", r, h.clock)
		}
	})

	t.Run("active:false ends tracking until a new domain is reported", func(t *testing.T) {
		h := setupWeb(t)
		h.report("a.com", true)
		h.clock += 3_000
		h.web.Handle(nil, false, true)
		h.clock += 3_000
		h.web.Sync(true)
		// on the page until the browser reported leaving
		if got := h.wantCount(1)[0].endTs; got != 1_003_000 {
			t.Errorf("end_ts = %d, want 1003000", got)
		}
	})

	t.Run("a reported domain is not counted until a browser is focused", func(t *testing.T) {
		h := setupWeb(t)
		h.report("a.com", false)
		h.wantCount(0)
	})

	t.Run("closeAt ends the session where idleness began, not later", func(t *testing.T) {
		h := setupWeb(t)
		h.report("a.com", true)
		for range 4 {
			h.clock += 5_000
			h.web.Sync(true)
		}
		h.web.CloseAt(1_000_000 + 8_000)
		if got := h.wantCount(1)[0].endTs; got != 1_008_000 {
			t.Errorf("end_ts = %d, want 1008000", got)
		}
	})

	t.Run("a long heartbeat gap splits the session", func(t *testing.T) {
		h := setupWeb(t)
		h.report("a.com", true)
		h.clock += 5_000
		h.web.Sync(true)
		h.clock += 8 * 3_600_000
		h.web.Sync(true)
		if got := h.wantCount(2)[0].endTs; got != 1_005_000 {
			t.Errorf("end_ts = %d, want 1005000", got)
		}
	})
}
