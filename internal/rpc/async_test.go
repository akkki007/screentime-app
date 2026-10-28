package rpc

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

// slowHandler has one method that blocks until released.
type slowHandler struct {
	fakeHandler
	release chan struct{}
}

func (h *slowHandler) Handle(method string, params json.RawMessage) (any, error) {
	if method == "planner.suggest" {
		<-h.release
		return map[string]bool{"slow": true}, nil
	}
	return h.fakeHandler.Handle(method, params)
}

func (h *slowHandler) IsAsync(method string) bool { return method == "planner.suggest" }

// A slow async method does not hold up the same connection's other requests,
// and its response comes back by id when it finishes.
func TestAsyncMethodsDoNotBlockTheConnection(t *testing.T) {
	h := &slowHandler{release: make(chan struct{})}
	path := filepath.Join(shortDir(t), "screentime", "daemon.sock")
	s, err := Listen(path, h)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)

	c := dial(t, path)
	c.send(t, `{"jsonrpc":"2.0","id":1,"method":"planner.suggest","params":{}}`)
	c.send(t, `{"jsonrpc":"2.0","id":2,"method":"tracker.resume"}`)
	if m := c.next(t); m["id"] != 2.0 {
		t.Fatalf("first answer was %v, want the quick request (id 2)", m)
	}
	close(h.release)
	if m := c.next(t); m["id"] != 1.0 {
		t.Fatalf("second answer was %v, want the slow request (id 1)", m)
	}
}
