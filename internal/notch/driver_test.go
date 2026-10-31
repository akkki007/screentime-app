package notch

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/akkki007/screentime-app/internal/ipc"
	"github.com/akkki007/screentime-app/internal/rpc"
	"github.com/akkki007/screentime-app/internal/shell"
)

// fakeDaemon answers what a notch asks for and records its actions.
type fakeDaemon struct {
	mu      sync.Mutex
	status  ipc.TrackerStatus
	actions []string
}

func (f *fakeDaemon) Handle(method string, params json.RawMessage) (any, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	id := int64(7)
	switch method {
	case "tracker.status":
		return f.status, nil
	case "apps.list":
		name := "Brave"
		return []map[string]any{{"appId": "brave_brave", "name": &name}}, nil
	case "limits.list":
		return []ipc.Limit{{ID: &id, TargetType: "app", Target: "brave_brave", DailyMs: 5_400_000, Action: "notify"}}, nil
	case "usage.summary":
		return []map[string]any{{"key": "brave_brave", "ms": 6_720_000}, {"key": "dev.zed.Zed", "ms": 1_440_000}}, nil
	case "tracker.pause", "tracker.resume", "focus.start", "focus.stop":
		f.actions = append(f.actions, method+" "+string(params))
		return map[string]bool{"ok": true}, nil
	}
	return nil, rpc.ErrMethodNotFound
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestDriverFollowsTheDaemon(t *testing.T) {
	path := filepath.Join(t.TempDir(), "run", "daemon.sock")
	fake := &fakeDaemon{}
	srv, err := rpc.Listen(path, fake)
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	d := NewDriver(func() {}, shell.Options{SocketPath: func() string { return path }, Retry: 50 * time.Millisecond})
	d.model.SetOptions(Options{}) // no greeting name from the test machine
	d.Start()
	defer d.Stop()

	eventually(t, "today's total", func() bool { return d.View().Text == "2h 16m" })
	p := d.Panel()
	if len(p.Apps) != 2 || p.Apps[0].Name != "Brave" || p.Apps[1].Name != "dev.zed.Zed" {
		t.Errorf("panel = %+v", p)
	}

	srv.Broadcast("event.limitHit", ipc.LimitHitEvent{LimitID: 7, Action: "notify"})
	eventually(t, "the limit alert", func() bool { return d.View().Kind == "alert" })
	if v := d.View(); v.Detail != "Brave · 1h 30m" {
		t.Errorf("alert = %+v", v)
	}

	until := time.Now().Add(25 * time.Minute).UnixMilli()
	srv.Broadcast("event.status", ipc.TrackerStatus{FocusMode: ipc.FocusMode{Active: true, Until: &until}})
	d.mu.Lock()
	d.model.temp = nil // skip the rest of the alert
	d.mu.Unlock()
	eventually(t, "focus mode", func() bool { return d.View().Kind == "focus" })

	if err := d.ToggleFocus(); err != nil {
		t.Fatal(err)
	}
	if err := d.TogglePause(); err != nil {
		t.Fatal(err)
	}
	fake.mu.Lock()
	got := strings.Join(fake.actions, "; ")
	fake.mu.Unlock()
	if got != `focus.stop ; tracker.pause {"minutes":15}` {
		t.Errorf("actions = %s", got)
	}

	srv.Broadcast("event.reminder", ipc.ReminderEvent{Kind: "break", Message: "Time for a break."})
	eventually(t, "the reminder", func() bool { return d.View().Kind == "reminder" })

	srv.Close()
	eventually(t, "offline", func() bool {
		d.mu.Lock()
		d.model.temp = nil
		d.mu.Unlock()
		return d.View().Kind == "offline"
	})
}
