// Command soak watches a running screentimed for a long soak test (issue
// #24): it samples the daemon's resident memory on an interval and, at the
// end, checks the database for the faults a soak is looking for. It is a dev
// tool and is not shipped.
//
//	go run ./scripts/soak -for 24h -every 5m          # sample RSS, then verify
//	go run ./scripts/soak -verify-only                # just check the database
//	go run ./scripts/soak -for 1m -every 10s -pid 123 # a specific process
//
// Verification reports, for the sessions recorded since -since (default: the
// start of the run): overlapping sessions (double counting), sessions that
// end before they start, and gaps longer than -gap, which are expected while
// idle, locked or suspended and are listed for a human to match against what
// actually happened.
package main

import (
	"bufio"
	"database/sql"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/akkki007/screentime-app/internal/store"
)

func main() {
	forDur := flag.Duration("for", 0, "how long to sample RSS (0 = verify only)")
	every := flag.Duration("every", 5*time.Minute, "RSS sampling interval")
	pid := flag.Int("pid", 0, "screentimed pid (default: pgrep -x screentimed)")
	dbPath := flag.String("db", store.DefaultPath(), "database to verify")
	gap := flag.Duration("gap", 2*time.Minute, "report gaps between sessions longer than this")
	since := flag.String("since", "", "verify sessions starting after this time (RFC 3339); default: soak start")
	verifyOnly := flag.Bool("verify-only", false, "skip sampling")
	flag.Parse()

	start := time.Now()
	var samples []sample
	if !*verifyOnly && *forDur > 0 {
		p := *pid
		if p == 0 {
			var err error
			if p, err = findDaemon(); err != nil {
				fatal(err)
			}
		}
		var err error
		if samples, err = sampleRSS(p, *forDur, *every); err != nil {
			fatal(err)
		}
	}
	from := start
	if *since != "" {
		t, err := time.Parse(time.RFC3339, *since)
		if err != nil {
			fatal(err)
		}
		from = t
	} else if *verifyOnly {
		from = time.Time{}
	}

	report(samples)
	if !verify(*dbPath, from, *gap) {
		os.Exit(1)
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "soak:", err)
	os.Exit(2)
}

type sample struct {
	at  time.Time
	kib int
}

func findDaemon() (int, error) {
	out, err := exec.Command("pgrep", "-x", "screentimed").Output()
	if err != nil {
		return 0, fmt.Errorf("no running screentimed found (start it, or pass -pid)")
	}
	return strconv.Atoi(strings.Fields(string(out))[0])
}

// rssKiB reads VmRSS from /proc/<pid>/status.
func rssKiB(pid int) (int, error) {
	f, err := os.Open(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return 0, err
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		if rest, ok := strings.CutPrefix(s.Text(), "VmRSS:"); ok {
			return strconv.Atoi(strings.Fields(rest)[0])
		}
	}
	return 0, fmt.Errorf("no VmRSS for pid %d", pid)
}

func sampleRSS(pid int, total, every time.Duration) ([]sample, error) {
	var out []sample
	take := func() error {
		kib, err := rssKiB(pid)
		if err != nil {
			return err
		}
		out = append(out, sample{time.Now(), kib})
		fmt.Printf("%s  rss %.1f MB\n", out[len(out)-1].at.Format(time.RFC3339), float64(kib)/1024)
		return nil
	}
	if err := take(); err != nil {
		return nil, err
	}
	end := time.Now().Add(total)
	for time.Now().Before(end) {
		time.Sleep(min(every, time.Until(end)))
		if err := take(); err != nil {
			return out, fmt.Errorf("daemon went away during the soak: %w", err)
		}
	}
	return out, nil
}

func report(samples []sample) {
	if len(samples) == 0 {
		return
	}
	lo, hi, sum := samples[0].kib, samples[0].kib, 0
	for _, s := range samples {
		lo, hi = min(lo, s.kib), max(hi, s.kib)
		sum += s.kib
	}
	mb := func(k int) float64 { return float64(k) / 1024 }
	fmt.Printf("\nRSS over %s (%d samples): start %.1f MB, end %.1f MB, min %.1f, max %.1f, mean %.1f (budget 60 MB)\n",
		samples[len(samples)-1].at.Sub(samples[0].at).Round(time.Second), len(samples),
		mb(samples[0].kib), mb(samples[len(samples)-1].kib), mb(lo), mb(hi), mb(sum/len(samples)))
}

// verify returns false if it found a fault (not for gaps, which are listed
// for review).
func verify(path string, from time.Time, gap time.Duration) bool {
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		fatal(err)
	}
	defer db.Close()

	rows, err := db.Query(`SELECT s.id, a.app_id, s.start_ts, s.end_ts
		FROM sessions s JOIN apps a ON a.id = s.app_id
		WHERE s.start_ts >= ? ORDER BY s.start_ts, s.id`, from.UnixMilli())
	if err != nil {
		fatal(err)
	}
	defer rows.Close()

	type sess struct {
		id         int64
		app        string
		start, end int64
	}
	var prev *sess
	var count, bad int
	var tracked time.Duration
	var gaps []string
	for rows.Next() {
		var s sess
		if err := rows.Scan(&s.id, &s.app, &s.start, &s.end); err != nil {
			fatal(err)
		}
		count++
		tracked += time.Duration(max(s.end-s.start, 0)) * time.Millisecond
		if s.end < s.start {
			bad++
			fmt.Printf("FAULT session %d (%s) ends before it starts\n", s.id, s.app)
		}
		if prev != nil {
			switch d := time.Duration(s.start-prev.end) * time.Millisecond; {
			case d < 0:
				bad++
				fmt.Printf("FAULT sessions %d (%s) and %d (%s) overlap by %s: double counting\n",
					prev.id, prev.app, s.id, s.app, -d.Round(time.Millisecond))
			case d > gap:
				gaps = append(gaps, fmt.Sprintf("  %s -> %s (%s)",
					time.UnixMilli(prev.end).Format("15:04:05"), time.UnixMilli(s.start).Format("15:04:05"), d.Round(time.Second)))
			}
		}
		if prev == nil || s.end > prev.end {
			cp := s
			prev = &cp
		}
	}
	if err := rows.Err(); err != nil {
		fatal(err)
	}

	fmt.Printf("\n%d sessions, %s tracked, %d faults, %d gaps over %s\n", count, tracked.Round(time.Second), bad, len(gaps), gap)
	if len(gaps) > 0 {
		fmt.Println("Gaps (expected while idle, locked, suspended or paused; check the rest by hand):")
		fmt.Println(strings.Join(gaps, "\n"))
	}
	return bad == 0
}
