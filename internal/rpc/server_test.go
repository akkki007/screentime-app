package rpc

import (
	"bufio"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// fakeHandler stands in for the daemon: a few methods plus a record of
// browser reports.
type fakeHandler struct {
	mu       sync.Mutex
	received []string
}

func (h *fakeHandler) Handle(method string, params json.RawMessage) (any, error) {
	switch method {
	case "tracker.resume":
		return map[string]bool{"ok": true}, nil
	case "tracker.pause":
		var p struct{ Minutes int }
		json.Unmarshal(params, &p)
		if p.Minutes <= 0 {
			return nil, errors.New("minutes: Number must be greater than 0")
		}
		return map[string]int{"resumeAt": p.Minutes}, nil
	case "limits.list":
		return nil, errors.New("boom")
	case "browser.activeTab":
		h.mu.Lock()
		h.received = append(h.received, string(params))
		h.mu.Unlock()
		return nil, nil
	}
	return nil, ErrMethodNotFound
}

// shortDir is a private temp dir with a path short enough for a Unix socket.
func shortDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "st-rpc-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

func start(t *testing.T) (*Server, string, *fakeHandler) {
	t.Helper()
	path := filepath.Join(shortDir(t), "screentime", "daemon.sock")
	h := &fakeHandler{}
	s, err := Listen(path, h)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	return s, path, h
}

type testClient struct {
	conn  net.Conn
	lines *bufio.Scanner
}

func dial(t *testing.T, path string) *testClient {
	t.Helper()
	conn, err := net.Dial("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return &testClient{conn, bufio.NewScanner(conn)}
}

func (c *testClient) send(t *testing.T, line string) {
	t.Helper()
	if _, err := c.conn.Write([]byte(line + "\n")); err != nil {
		t.Fatal(err)
	}
}

func (c *testClient) next(t *testing.T) map[string]any {
	t.Helper()
	c.conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if !c.lines.Scan() {
		t.Fatalf("no message: %v", c.lines.Err())
	}
	var m map[string]any
	if err := json.Unmarshal(c.lines.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func errorOf(m map[string]any) (float64, string) {
	e, _ := m["error"].(map[string]any)
	code, _ := e["code"].(float64)
	msg, _ := e["message"].(string)
	return code, msg
}

func TestSocketIsPrivate(t *testing.T) {
	old := syscall.Umask(0o022)
	defer syscall.Umask(old)
	_, path, _ := start(t)
	for p, want := range map[string]os.FileMode{path: 0o600, filepath.Dir(path): 0o700} {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != want {
			t.Errorf("%s mode = %o, want %o", p, got, want)
		}
	}
}

func TestIsLive(t *testing.T) {
	s, path, _ := start(t)
	if !IsLive(path) {
		t.Error("running server not detected")
	}
	s.Close()
	if IsLive(path) {
		t.Error("closed server still detected")
	}
	if IsLive(filepath.Join(t.TempDir(), "never.sock")) {
		t.Error("missing socket detected")
	}
}

func TestHandshake(t *testing.T) {
	_, path, _ := start(t)
	c := dial(t, path)
	c.send(t, `{"jsonrpc":"2.0","id":1,"method":"version","params":{"major":1}}`)
	if got := c.next(t)["result"]; !reflect.DeepEqual(got, map[string]any{"major": 1.0}) {
		t.Errorf("result = %v", got)
	}
	c.send(t, `{"jsonrpc":"2.0","id":2,"method":"version","params":{"major":99}}`)
	if code, msg := errorOf(c.next(t)); code != CodeVersion || msg != "unsupported major version 99" {
		t.Errorf("error = %v %q", code, msg)
	}
}

func TestResultsAndErrors(t *testing.T) {
	_, path, _ := start(t)
	c := dial(t, path)
	c.send(t, `{"jsonrpc":"2.0","id":1,"method":"tracker.pause","params":{"minutes":5}}`)
	if got := c.next(t)["result"]; !reflect.DeepEqual(got, map[string]any{"resumeAt": 5.0}) {
		t.Errorf("result = %v", got)
	}
	c.send(t, `{"jsonrpc":"2.0","id":2,"method":"tracker.pause","params":{"minutes":-1}}`)
	if _, msg := errorOf(c.next(t)); !strings.HasPrefix(msg, "minutes: ") {
		t.Errorf("message = %q", msg)
	}
	c.send(t, `{"jsonrpc":"2.0","id":3,"method":"limits.list"}`)
	if code, msg := errorOf(c.next(t)); code != CodeInternal || msg != "boom" {
		t.Errorf("error = %v %q", code, msg)
	}
	c.send(t, `{"jsonrpc":"2.0","id":"x","method":"no.such.method"}`)
	m := c.next(t)
	if code, _ := errorOf(m); code != CodeMethodNotFound || m["id"] != "x" {
		t.Errorf("response = %v", m)
	}
}

func TestSurvivesGarbage(t *testing.T) {
	_, path, h := start(t)
	c := dial(t, path)
	c.send(t, "not json\nnull\n42\n[1,2]\n{\"method\":7}")
	c.send(t, `{"jsonrpc":"2.0","method":"browser.activeTab","params":{"domain":"a.com","active":true}}`)
	c.send(t, `{"jsonrpc":"2.0","id":1,"method":"tracker.resume"}`)
	if got := c.next(t)["result"]; !reflect.DeepEqual(got, map[string]any{"ok": true}) {
		t.Errorf("result = %v", got)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.received) != 1 {
		t.Errorf("browser reports = %v", h.received)
	}
}

func TestBroadcast(t *testing.T) {
	s, path, _ := start(t)
	a, b := dial(t, path), dial(t, path)
	for _, c := range []*testClient{a, b} {
		c.send(t, `{"jsonrpc":"2.0","id":1,"method":"tracker.resume"}`)
		c.next(t) // registered
	}
	s.Broadcast("event.focus", map[string]any{"appId": "org.mozilla.firefox", "since": 123})
	want := map[string]any{"jsonrpc": "2.0", "method": "event.focus",
		"params": map[string]any{"appId": "org.mozilla.firefox", "since": 123.0}}
	for _, c := range []*testClient{a, b} {
		if got := c.next(t); !reflect.DeepEqual(got, want) {
			t.Errorf("notification = %v", got)
		}
	}
}

// S7: an oversized line disconnects the client; others are unaffected.
func TestOversizedLineDisconnects(t *testing.T) {
	_, path, _ := start(t)
	c := dial(t, path)
	big := strings.Repeat("x", MaxLineBytes+10)
	c.conn.Write([]byte(big)) // no newline: the server must not buffer forever
	c.conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := c.conn.Read(make([]byte, 1)); err == nil {
		t.Error("connection still open after an oversized line")
	}
	ok := dial(t, path)
	ok.send(t, `{"jsonrpc":"2.0","id":1,"method":"tracker.resume"}`)
	ok.next(t)
}

// A client that never reads is dropped instead of blocking broadcasts.
func TestSlowClientIsDropped(t *testing.T) {
	s, path, _ := start(t)
	slow := dial(t, path)
	slow.send(t, `{"jsonrpc":"2.0","id":1,"method":"tracker.resume"}`)
	time.Sleep(50 * time.Millisecond) // registered; never read from again
	done := make(chan struct{})
	go func() {
		payload := strings.Repeat("y", 64*1024)
		for i := 0; i < clientQueue*4; i++ {
			s.Broadcast("event.status", payload)
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Broadcast blocked on a client that doesn't read")
	}
}

// S2: the socket directory must be a real directory owned by this user.
func TestRefusesUnsafeSocketDir(t *testing.T) {
	t.Run("symlink", func(t *testing.T) {
		base := shortDir(t)
		target := filepath.Join(base, "elsewhere")
		os.Mkdir(target, 0o700)
		os.Symlink(target, filepath.Join(base, "screentime"))
		if _, err := Listen(filepath.Join(base, "screentime", "daemon.sock"), &fakeHandler{}); err == nil {
			t.Error("listened through a symlinked directory")
		}
	})
	t.Run("loose mode is tightened", func(t *testing.T) {
		base := shortDir(t)
		dir := filepath.Join(base, "screentime")
		os.Mkdir(dir, 0o777)
		os.Chmod(dir, 0o777)
		s, err := Listen(filepath.Join(dir, "daemon.sock"), &fakeHandler{})
		if err != nil {
			t.Fatal(err)
		}
		defer s.Close()
		info, _ := os.Stat(dir)
		if info.Mode().Perm() != 0o700 {
			t.Errorf("dir mode = %o, want 700", info.Mode().Perm())
		}
	})
	t.Run("a non-socket file is not replaced", func(t *testing.T) {
		base := shortDir(t)
		dir := filepath.Join(base, "screentime")
		os.Mkdir(dir, 0o700)
		path := filepath.Join(dir, "daemon.sock")
		os.WriteFile(path, []byte("keep me"), 0o600)
		if _, err := Listen(path, &fakeHandler{}); err == nil {
			t.Error("replaced a regular file")
		}
		if b, _ := os.ReadFile(path); string(b) != "keep me" {
			t.Error("regular file was modified")
		}
	})
	t.Run("a stale socket is replaced", func(t *testing.T) {
		base := shortDir(t)
		path := filepath.Join(base, "screentime", "daemon.sock")
		first, err := Listen(path, &fakeHandler{})
		if err != nil {
			t.Fatal(err)
		}
		// Simulate a crash: the socket file stays behind with nobody listening.
		first.listener.SetUnlinkOnClose(false)
		first.Close()
		second, err := Listen(path, &fakeHandler{})
		if err != nil {
			t.Fatalf("stale socket not replaced: %v", err)
		}
		second.Close()
	})
}

func TestSocketPathIgnoresUntrustedRuntimeDir(t *testing.T) {
	loose := shortDir(t)
	os.Chmod(loose, 0o777)
	t.Setenv("XDG_RUNTIME_DIR", loose)
	if got := SocketPath(); strings.HasPrefix(got, loose) {
		t.Errorf("used world-writable XDG_RUNTIME_DIR: %s", got)
	}
	private := shortDir(t)
	os.Chmod(private, 0o700)
	t.Setenv("XDG_RUNTIME_DIR", private)
	if got, want := SocketPath(), filepath.Join(private, "screentime", "daemon.sock"); got != want {
		t.Errorf("SocketPath = %s, want %s", got, want)
	}
}
