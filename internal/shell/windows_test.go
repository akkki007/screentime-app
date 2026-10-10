package shell

import (
	"sync"
	"testing"
	"time"
)

// sleeper is a stand-in window process: the first process for a kind runs
// until it is signalled, and any further one (a poke at the running window)
// exits at once, as the real second instance does.
func sleeper() *Windows {
	var mu sync.Mutex
	seen := map[string]bool{}
	return &Windows{Exe: "/bin/sleep", Args: func(kind string) []string {
		mu.Lock()
		defer mu.Unlock()
		if seen[kind] {
			return []string{"0.05"}
		}
		seen[kind] = true
		return []string{"30"}
	}}
}

func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestToggleOpensAndCloses(t *testing.T) {
	var mu sync.Mutex
	var exits []string
	w := sleeper()
	w.OnExit = func(kind string) { mu.Lock(); exits = append(exits, kind); mu.Unlock() }

	if err := w.Toggle("panel"); err != nil {
		t.Fatal(err)
	}
	if !w.Running("panel") {
		t.Fatal("panel should be running after the first toggle")
	}
	if err := w.Toggle("panel"); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "panel to exit", func() bool { return !w.Running("panel") })
	mu.Lock()
	defer mu.Unlock()
	if len(exits) != 1 || exits[0] != "panel" {
		t.Fatalf("OnExit calls = %v, want [panel]", exits)
	}
}

func TestToggleRightAfterCloseIsTheClosingClick(t *testing.T) {
	w := sleeper()
	if err := w.Toggle("panel"); err != nil {
		t.Fatal(err)
	}
	w.Close("panel")
	waitUntil(t, "panel to exit", func() bool { return !w.Running("panel") })

	// The click that took focus away from the panel arrives right after it
	// closed itself; it must not reopen it.
	if err := w.Toggle("panel"); err != nil {
		t.Fatal(err)
	}
	if w.Running("panel") {
		t.Fatal("a toggle within the grace period reopened the panel")
	}

	time.Sleep(toggleGrace + 50*time.Millisecond)
	if err := w.Toggle("panel"); err != nil {
		t.Fatal(err)
	}
	if !w.Running("panel") {
		t.Fatal("a toggle after the grace period should open the panel")
	}
	w.CloseAll()
	waitUntil(t, "panel to exit", func() bool { return !w.Running("panel") })
}

func TestOpenWhileRunningDoesNotReplaceTheWindow(t *testing.T) {
	w := sleeper()
	if err := w.Open("main"); err != nil {
		t.Fatal(err)
	}
	first := w.running["main"]
	// The second invocation is a poke that exits on its own; here it is just
	// another sleeper, which must not be tracked as the window.
	if err := w.Open("main"); err != nil {
		t.Fatal(err)
	}
	if w.running["main"] != first {
		t.Fatal("Open replaced the running window's process")
	}
	w.CloseAll()
	waitUntil(t, "main to exit", func() bool { return !w.Running("main") })
}

func TestKindsAreIndependent(t *testing.T) {
	w := sleeper()
	if err := w.Open("main"); err != nil {
		t.Fatal(err)
	}
	if w.Running("panel") {
		t.Fatal("opening main must not mark panel as running")
	}
	w.Close("panel") // no-op
	if !w.Running("main") {
		t.Fatal("closing panel must not close main")
	}
	w.CloseAll()
	waitUntil(t, "main to exit", func() bool { return !w.Running("main") })
}

func TestStartFailureIsReported(t *testing.T) {
	w := &Windows{Exe: "/nonexistent/screentime", Args: func(string) []string { return nil }}
	if err := w.Open("main"); err == nil {
		t.Fatal("expected an error for a missing executable")
	}
	if w.Running("main") {
		t.Fatal("a failed start must not be tracked")
	}
}
