package notify

import "log"

// Log writes notifications to the daemon's log. It stands in on the OSes
// without a notifier yet (Windows, macOS), so a limit or break reminder is
// at least recorded rather than lost.
type Log struct{}

func (Log) Notify(title, body string) {
	log.Printf("[notifier] (no desktop notifications on this OS yet) %s: %s", title, body)
}
