// Package notify sends desktop notifications.
package notify

// Notifier shows a notification. Failures are the notifier's to log: a
// missing notification daemon must never break the rules engine.
type Notifier interface {
	Notify(title, body string)
}

// Discard drops every notification.
type Discard struct{}

func (Discard) Notify(string, string) {}
