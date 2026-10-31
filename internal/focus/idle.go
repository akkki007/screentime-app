package focus

import (
	"log"
	"time"
)

// idlePollEvery is how often a polling provider checks the idle time: a
// quarter of the threshold, so a crossing is noticed well within it, but never
// faster than every second or slower than every five. The thresholds the UI
// offers are whole minutes, so in practice this is 5 s: one wake-up per
// interval, each a single cheap system call. Providers with an idle
// notification (GNOME, KDE) don't poll at all.
func idlePollEvery(thresholdMs int64) time.Duration {
	every := time.Duration(thresholdMs/4) * time.Millisecond
	return min(max(every, time.Second), 5*time.Second)
}

// pollIdle calls query every interval until done is closed, and reports each
// crossing of thresholdMs (not every poll). The first failure is logged,
// prefixed with name.
func pollIdle(name string, done <-chan struct{}, every time.Duration, thresholdMs int64, query func() (int64, error), cb func(bool)) {
	ticker := time.NewTicker(every)
	defer ticker.Stop()
	idle, warned := false, false
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			ms, err := query()
			if err != nil {
				if !warned {
					log.Printf("[%s] reading idle time: %v", name, err)
					warned = true
				}
				continue
			}
			if now := ms >= thresholdMs; now != idle {
				idle = now
				cb(idle)
			}
		}
	}
}
