package rpc

import (
	"bufio"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
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
	base := "/tmp"
	if runtime.GOOS == "windows" {
		base = "" // no /tmp; the default temp dir is short enough for AF_UNIX
	}
	dir, err := os.MkdirTemp(base, "st-rpc-")
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
