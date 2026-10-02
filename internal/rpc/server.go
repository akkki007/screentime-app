// Package rpc serves JSON-RPC 2.0 over a Unix socket, one JSON message per
// line (docs/architecture.md#ipc-contracts). Port of
// apps/daemon/src/rpc-server.ts.
//
// Security requirements (docs/migration-to-go.md):
//   - S2: the socket directory must be a real directory owned by this user,
//     made 0700; the socket is created 0600 by the umask, with no
//     path-based chmod; an untrusted XDG_RUNTIME_DIR is ignored.
//   - S7: lines over MaxLineBytes disconnect the client, and every request
//     is validated by the handler.
package rpc

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/akkki007/screentime-app/internal/ipc"
)

// MaxLineBytes: a longer line is a misbehaving client; drop it rather than
// buffer forever.
const MaxLineBytes = 1_000_000

// clientQueue is how many outgoing messages may wait for a slow client
// before it is disconnected, so a client that stops reading can't block the
// daemon.
const clientQueue = 256

// JSON-RPC error codes.
const (
	CodeVersion        = -32000
	CodeMethodNotFound = -32601
	CodeInternal       = -32603
)

// ErrMethodNotFound is returned by a Handler for an unknown method.
var ErrMethodNotFound = errors.New("method not found")

// Handler answers requests. It is called for notifications too (requests
// without an id); their result is discarded.
type Handler interface {
	Handle(method string, params json.RawMessage) (any, error)
}

// SocketPath is $XDG_RUNTIME_DIR/screentime/daemon.sock. XDG_RUNTIME_DIR is
// trusted only if it is a directory owned by this user with mode 0700 (as
// GLib requires); otherwise /run/user/<uid> is used.
func SocketPath() string {
	base := os.Getenv("XDG_RUNTIME_DIR")
	if base == "" || !privateTo(base, os.Getuid()) {
		if base != "" {
			log.Printf("[rpc] ignoring XDG_RUNTIME_DIR=%s: not a 0700 directory owned by this user", base)
		}
		base = fmt.Sprintf("/run/user/%d", os.Getuid())
	}
	return filepath.Join(base, "screentime", "daemon.sock")
}

func privateTo(dir string, uid int) bool {
	info, err := os.Lstat(dir)
	if err != nil || !info.IsDir() {
		return false
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	return ok && int(st.Uid) == uid && info.Mode().Perm()&0o077 == 0
}

// IsLive reports whether a daemon is already accepting connections at path.
func IsLive(path string) bool {
	conn, err := net.DialTimeout("unix", path, time.Second)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// prepareDir creates the socket's directory, or checks an existing one: it
// must be a directory (not a symlink) owned by this user. Group and other
// permissions are removed.
func prepareDir(dir string) error {
	if err := os.Mkdir(dir, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory (symlink?); refusing to use it", dir)
	}
	if st, ok := info.Sys().(*syscall.Stat_t); !ok || int(st.Uid) != os.Getuid() {
		return fmt.Errorf("%s is owned by another user; refusing to use it", dir)
	}
	if info.Mode().Perm()&0o077 != 0 {
		log.Printf("[rpc] %s was mode %o; setting 0700", dir, info.Mode().Perm())
		return os.Chmod(dir, 0o700)
	}
	return nil
}

// removeStale deletes a leftover socket from a daemon that is gone. Anything
// that isn't a socket is left alone and reported.
func removeStale(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("%s exists and is not a socket; refusing to replace it", path)
	}
	return os.Remove(path)
}

// Server is a running JSON-RPC server.
type Server struct {
	listener *net.UnixListener
	handler  Handler

	mu      sync.Mutex
	clients map[*client]struct{}
	wg      sync.WaitGroup
}

type client struct {
	conn net.Conn
	out  chan []byte
	once sync.Once
}

func (c *client) close() { c.once.Do(func() { c.conn.Close(); close(c.out) }) }

// Listen binds the socket at path. The caller checks IsLive first: a second
// daemon would steal the first one's socket and double count everything.
func Listen(path string, handler Handler) (*Server, error) {
	if err := prepareDir(filepath.Dir(path)); err != nil {
		return nil, err
	}
	if err := removeStale(path); err != nil {
		return nil, err
	}
	// bind creates the socket as 0777 minus the umask: 0600 here, with no
	// path-based chmod afterwards.
	old := syscall.Umask(0o177)
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	syscall.Umask(old)
	if err != nil {
		return nil, err
	}
	s := &Server{listener: ln, handler: handler, clients: map[*client]struct{}{}}
	s.wg.Add(1)
	go s.accept()
	return s, nil
}

func (s *Server) accept() {
	defer s.wg.Done()
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			return // closed
		}
		c := &client{conn: conn, out: make(chan []byte, clientQueue)}
		s.mu.Lock()
		s.clients[c] = struct{}{}
		s.mu.Unlock()
		s.wg.Add(2)
		go s.write(c)
		go s.read(c)
	}
}

func (s *Server) write(c *client) {
	defer s.wg.Done()
	for line := range c.out {
		if _, err := c.conn.Write(line); err != nil {
			s.drop(c)
		}
	}
}

func (s *Server) read(c *client) {
	defer s.wg.Done()
	defer s.drop(c)
	scanner := bufio.NewScanner(c.conn)
	scanner.Buffer(make([]byte, 0, 64*1024), MaxLineBytes)
	for scanner.Scan() {
		if line := strings.TrimSpace(scanner.Text()); line != "" {
			s.handleLine(c, []byte(line))
		}
	}
	if errors.Is(scanner.Err(), bufio.ErrTooLong) {
		log.Print("[rpc] client sent an oversized message; disconnecting")
	}
}

func (s *Server) drop(c *client) {
	s.mu.Lock()
	delete(s.clients, c)
	s.mu.Unlock()
	c.close()
}

// send queues a line for one client, disconnecting it if its queue is full.
func (s *Server) send(c *client, line []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.clients[c]; !ok {
		return
	}
	select {
	case c.out <- line:
	default:
		log.Print("[rpc] client is not reading; disconnecting")
		delete(s.clients, c)
		c.close()
	}
}

// Broadcast sends a daemon → client notification to every client.
func (s *Server) Broadcast(method string, params any) {
	line, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
	if err != nil {
		log.Printf("[rpc] encoding %s: %v", method, err)
		return
	}
	line = append(line, '\n')
	s.mu.Lock()
	clients := make([]*client, 0, len(s.clients))
	for c := range s.clients {
		clients = append(clients, c)
	}
	s.mu.Unlock()
	for _, c := range clients {
		s.send(c, line)
	}
}

// Close stops accepting, disconnects every client and removes the socket.
func (s *Server) Close() {
	s.listener.Close() // also unlinks the socket file
	s.mu.Lock()
	for c := range s.clients {
		delete(s.clients, c)
		c.close()
	}
	s.mu.Unlock()
	s.wg.Wait()
}

type request struct {
	ID     json.RawMessage `json:"id"`
	Method *string         `json:"method"`
	Params json.RawMessage `json:"params"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

func (s *Server) handleLine(c *client, line []byte) {
	var req request
	// Anything that isn't a JSON object with a string method is ignored.
	if err := json.Unmarshal(line, &req); err != nil || req.Method == nil {
		return
	}
	notification := len(req.ID) == 0 || string(req.ID) == "null"
	respond := func(result any, rerr *rpcError) {
		if notification {
			return
		}
		res := response{JSONRPC: "2.0", ID: req.ID, Error: rerr}
		if rerr == nil {
			// Marshal now so a nil result still appears as "result".
			raw, err := json.Marshal(result)
			if err != nil {
				res.Error = &rpcError{CodeInternal, err.Error()}
			} else {
				res.Result = json.RawMessage(raw)
			}
		}
		out, err := json.Marshal(res)
		if err != nil {
			log.Printf("[rpc] encoding response: %v", err)
			return
		}
		s.send(c, append(out, '\n'))
	}

	method := *req.Method
	if method == "version" {
		major := versionParam(req.Params)
		if major != fmt.Sprint(ipc.Version) {
			respond(nil, &rpcError{CodeVersion, "unsupported major version " + major})
			return
		}
		respond(map[string]int{"major": ipc.Version}, nil)
		return
	}

	result, err := s.handler.Handle(method, req.Params)
	switch {
	case errors.Is(err, ErrMethodNotFound):
		respond(nil, &rpcError{CodeMethodNotFound, "method not found: " + method})
	case err != nil:
		respond(nil, &rpcError{CodeInternal, err.Error()})
	default:
		respond(result, nil)
	}
}

// versionParam renders params.major the way the Bun daemon's message did
// ("undefined" when missing).
func versionParam(params json.RawMessage) string {
	var p map[string]json.RawMessage
	if json.Unmarshal(params, &p) != nil {
		return "undefined"
	}
	raw, ok := p["major"]
	if !ok {
		return "undefined"
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	return string(raw)
}
