package tracker

import (
	"database/sql"
	"log"
)

type openWebSession struct {
	rowID      int64
	domain     string
	startTs    int64
	lastSeenTs int64
}

// WebTracker turns the browser extension's active-tab reports into
// web_sessions rows. A browser reports only when the tab changes, so time is
// extended by the daemon's heartbeat, and only while a browser is the app
// being counted. The last reported domain is remembered, so returning to the
// browser resumes it.
type WebTracker struct {
	db            *sql.DB
	now           func() int64
	open          *openWebSession
	currentDomain string
}

func NewWebTracker(db *sql.DB, now func() int64) *WebTracker {
	return &WebTracker{db: db, now: now}
}

// ActiveDomain is the domain being counted, or "".
func (w *WebTracker) ActiveDomain() string {
	if w.open == nil {
		return ""
	}
	return w.open.domain
}

// Handle records a report from the extension.
func (w *WebTracker) Handle(domain *string, active bool, tracking bool) {
	w.currentDomain = ""
	if active && domain != nil {
		w.currentDomain = *domain
	}
	w.Sync(tracking)
}

// Sync reconciles with whether a browser is being counted. Call it on every
// heartbeat.
func (w *WebTracker) Sync(tracking bool) {
	now := w.now()
	if w.open != nil {
		if now-w.open.lastSeenTs > mergeGapMs {
			// Heartbeats stalled (suspend): end where we last saw it.
			w.close()
		} else {
			// The user was on this page right up to now, even if leaving it.
			w.open.lastSeenTs = now
		}
	}
	if w.open != nil && (!tracking || w.open.domain != w.currentDomain) {
		w.close()
	}
	if w.open == nil && tracking && w.currentDomain != "" {
		res, err := w.db.Exec("INSERT INTO web_sessions (domain, start_ts, end_ts) VALUES (?, ?, ?)",
			w.currentDomain, now, now)
		if err != nil {
			log.Printf("[web] opening session: %v", err)
			return
		}
		id, _ := res.LastInsertId()
		w.open = &openWebSession{rowID: id, domain: w.currentDomain, startTs: now, lastSeenTs: now}
		return
	}
	if w.open != nil {
		w.open.lastSeenTs = now
		w.write()
	}
}

// CloseAt ends the open session at ts (e.g. when idleness began).
func (w *WebTracker) CloseAt(ts int64) {
	if w.open == nil {
		return
	}
	w.open.lastSeenTs = max(w.open.startTs, min(w.open.lastSeenTs, ts))
	w.close()
}

// Reset drops the open session without writing it, after its rows were wiped.
func (w *WebTracker) Reset() { w.open = nil }

func (w *WebTracker) close() {
	if w.open == nil {
		return
	}
	w.write()
	w.open = nil
}

func (w *WebTracker) write() {
	if _, err := w.db.Exec("UPDATE web_sessions SET end_ts = ? WHERE id = ?", w.open.lastSeenTs, w.open.rowID); err != nil {
		log.Printf("[web] extending session: %v", err)
	}
}
