package shell

import (
	"errors"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

// Windows runs each UI window as its own short-lived process.
//
// The tray process never creates a webview. A webview loads GL drivers and
// WebKit state that are never unloaded, so keeping windows in the tray's own
// process left an idle tray holding ~125 MB after the dashboard was closed
// (against ~40 MB if it had never been opened). A window process exits when
// its window closes, which hands all of that back.
type Windows struct {
	// Exe is the binary to run, normally os.Executable(). Args builds its
	// command line for a window kind ("main", "panel").
	Exe  string
	Args func(kind string) []string
	// OnExit is called, off the caller's goroutine, when a window's process
	// ends for any reason.
	OnExit func(kind string)

	mu       sync.Mutex
	running  map[string]*exec.Cmd
	lastExit map[string]time.Time
}

// toggleGrace is how soon after a window closed itself that a click on the
// tray is read as the click that closed it. The quick panel closes when it
// loses focus, and clicking the tray icon is what takes the focus away, so
// without this the same click would close the panel and open it again.
const toggleGrace = 400 * time.Millisecond

// ParentPipeEnv is set for window processes started by Windows. When it is
// present, stdin is a pipe from the tray and its closing means the tray is gone.
const ParentPipeEnv = "SCREENTIME_PARENT_PIPE"

// Running reports whether the window's process is alive.
func (w *Windows) Running(kind string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.running[kind] != nil
}

// Open starts the window's process, or asks the running one to raise itself:
// a second `screentime -window kind` finds the first through its single
// instance lock, tells it, and exits.
func (w *Windows) Open(kind string) error {
	w.mu.Lock()
	alive := w.running[kind] != nil
	w.mu.Unlock()
	if alive {
		cmd := w.command(kind)
		if err := cmd.Start(); err != nil {
			return err
		}
		go func() { _ = cmd.Wait() }()
		return nil
	}
	return w.start(kind)
}

// Toggle closes the window if it is open, and opens it otherwise.
func (w *Windows) Toggle(kind string) error {
	w.mu.Lock()
	cmd := w.running[kind]
	recent := !w.lastExit[kind].IsZero() && time.Since(w.lastExit[kind]) < toggleGrace
	w.mu.Unlock()
	switch {
	case cmd != nil:
		return cmd.Process.Signal(syscall.SIGTERM)
	case recent:
		return nil
	}
	return w.start(kind)
}

// Close asks the window to close, if it is open.
func (w *Windows) Close(kind string) {
	w.mu.Lock()
	cmd := w.running[kind]
	w.mu.Unlock()
	if cmd != nil {
		_ = cmd.Process.Signal(syscall.SIGTERM)
	}
}

// CloseAll asks every window to close.
func (w *Windows) CloseAll() {
	w.mu.Lock()
	var cmds []*exec.Cmd
	for _, c := range w.running {
		cmds = append(cmds, c)
	}
	w.mu.Unlock()
	for _, c := range cmds {
		_ = c.Process.Signal(syscall.SIGTERM)
	}
}

func (w *Windows) command(kind string) *exec.Cmd {
	cmd := exec.Command(w.Exe, w.Args(kind)...)
	// exec closes the write end when the tray exits, whatever the reason.
	_, _ = cmd.StdinPipe()
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	// A window never outlives the tray that opened it, even if the tray is
	// killed rather than quit: the child watches this pipe and quits when it
	// closes. (Pdeathsig would be simpler, but Go ties it to the OS thread
	// that forked, which the runtime may retire at any time.)
	cmd.Env = append(os.Environ(), ParentPipeEnv+"=1")
	return cmd
}

func (w *Windows) start(kind string) error {
	if w.Exe == "" || w.Args == nil {
		return errors.New("shell: Windows has no executable")
	}
	cmd := w.command(kind)
	w.mu.Lock()
	if w.running == nil {
		w.running = map[string]*exec.Cmd{}
		w.lastExit = map[string]time.Time{}
	}
	if err := cmd.Start(); err != nil {
		w.mu.Unlock()
		return err
	}
	w.running[kind] = cmd
	w.mu.Unlock()

	go func() {
		_ = cmd.Wait()
		w.mu.Lock()
		if w.running[kind] == cmd {
			delete(w.running, kind)
			w.lastExit[kind] = time.Now()
		}
		w.mu.Unlock()
		if w.OnExit != nil {
			w.OnExit(kind)
		}
	}()
	return nil
}
