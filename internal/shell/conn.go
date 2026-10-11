// Package shell is the desktop app's non-GUI core: the daemon connection,
// the tray menu model, safe export writing, and the allow-listed bridge the
// webview talks to (ADR 3: the webview never opens the daemon socket).
package shell

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/akkki007/screentime-app/internal/ipc"
	"github.com/akkki007/screentime-app/internal/rpc"
)

const (
	callTimeout = 10 * time.Second
	// maxLine bounds one daemon message; an export can be large.
	maxLine = 64 << 20
)

// ErrDaemonDown is what a call returns while there is no connection. The
// webview shows it as is.
var ErrDaemonDown = errors.New("The Screentime daemon is not running. Start it with `systemctl --user start screentime-daemon`.")

// State is the connection state the UI shows.
type State struct {
	Connected bool   `json:"connected"`
	Error     string `json:"error,omitempty"`
}

// Options configures a Conn.
type Options struct {
	// SocketPath returns the daemon socket; default rpc.SocketPath.
	SocketPath func() string
	// Dial connects after S3's ownership checks; default rpc.Dial. Tests
	// replace it.
	Dial func(path string) (net.Conn, error)
	// Retry is the delay between reconnect attempts (default 2 s).
	Retry          time.Duration
	OnNotification func(method string, params json.RawMessage)
	OnState        func(State)
}

// Conn keeps a client connected to the daemon for the life of the UI. The
// daemon can start after the UI, restart under it, or stop, and the UI just
// catches up when it returns.
type Conn struct {
	opts Options

	mu        sync.Mutex
	client    *client
	stopped   bool
	started   bool
	lastError string
	timer     *time.Timer
}

func NewConn(opts Options) *Conn {
	if opts.SocketPath == nil {
		opts.SocketPath = rpc.SocketPath
	}
	if opts.Dial == nil {
		opts.Dial = func(path string) (net.Conn, error) { return rpc.Dial(path) }
	}
	if opts.Retry == 0 {
		opts.Retry = 2 * time.Second
	}
	return &Conn{opts: opts, stopped: true}
}

// Start begins connecting (and reconnecting) in the background.
func (c *Conn) Start() {
	c.mu.Lock()
	if !c.stopped {
		c.mu.Unlock()
		return
	}
	c.stopped = false
	c.started = true
	c.mu.Unlock()
	go c.attempt()
}

// Stop disconnects and stops reconnecting.
func (c *Conn) Stop() {
	c.mu.Lock()
	c.stopped = true
	if c.timer != nil {
		c.timer.Stop()
	}
	cl := c.client
	c.client = nil
	c.mu.Unlock()
	if cl != nil {
		cl.close(errors.New("connection closed"))
	}
}

// State is the current connection state.
func (c *Conn) State() State {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.client != nil {
		return State{Connected: true}
	}
	return State{Error: c.lastError}
}

// Call sends a JSON-RPC request and returns the raw result.
func (c *Conn) Call(method string, params any) (json.RawMessage, error) {
	c.mu.Lock()
	cl := c.client
	c.mu.Unlock()
	if cl == nil {
		return nil, ErrDaemonDown
	}
	return cl.call(method, params)
}

func (c *Conn) emitState(s State) {
	if c.opts.OnState != nil {
		c.opts.OnState(s)
	}
}

func (c *Conn) attempt() {
	c.mu.Lock()
	if c.stopped {
		c.mu.Unlock()
		return
	}
	c.mu.Unlock()

	cl, err := c.dial()
	c.mu.Lock()
	if c.stopped {
		c.mu.Unlock()
		if cl != nil {
			cl.close(errors.New("connection closed"))
		}
		return
	}
	if err != nil {
		changed := err.Error() != c.lastError
		c.lastError = err.Error()
		c.mu.Unlock()
		// Only report a changed error: a missing daemon would otherwise
		// repeat itself on every retry.
		if changed {
			c.emitState(State{Error: err.Error()})
		}
		c.scheduleRetry()
		return
	}
	c.client = cl
	c.lastError = ""
	c.mu.Unlock()
	c.emitState(State{Connected: true})
}

func (c *Conn) dial() (*client, error) {
	conn, err := c.opts.Dial(c.opts.SocketPath())
	if err != nil {
		return nil, err
	}
	cl := newClient(conn)
	cl.onNotification = c.opts.OnNotification
	cl.onClose = func() {
		c.mu.Lock()
		if c.client != cl {
			c.mu.Unlock()
			return
		}
		c.client = nil
		c.mu.Unlock()
		c.emitState(State{})
		c.scheduleRetry()
	}
	go cl.readLoop()

	if _, err := cl.call("version", map[string]int{"major": ipc.Version}); err != nil {
		cl.close(err)
		return nil, err
	}
	return cl, nil
}

func (c *Conn) scheduleRetry() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stopped {
		return
	}
	c.timer = time.AfterFunc(c.opts.Retry, c.attempt)
}

// client is one established connection.
type client struct {
	conn net.Conn

	onNotification func(method string, params json.RawMessage)
	onClose        func()

	writeMu sync.Mutex
	mu      sync.Mutex
	nextID  int64
	pending map[int64]chan reply
	closed  bool
}

type reply struct {
	result json.RawMessage
	err    error
}

func newClient(conn net.Conn) *client {
	return &client{conn: conn, pending: map[int64]chan reply{}}
}

func (cl *client) call(method string, params any) (json.RawMessage, error) {
	cl.mu.Lock()
	if cl.closed {
		cl.mu.Unlock()
		return nil, ErrDaemonDown
	}
	cl.nextID++
	id := cl.nextID
	ch := make(chan reply, 1)
	cl.pending[id] = ch
	cl.mu.Unlock()

	req := map[string]any{"jsonrpc": "2.0", "id": id, "method": method}
	if params != nil {
		req["params"] = params
	}
	line, err := json.Marshal(req)
	if err != nil {
		cl.forget(id)
		return nil, err
	}
	cl.writeMu.Lock()
	_ = cl.conn.SetWriteDeadline(time.Now().Add(callTimeout))
	_, err = cl.conn.Write(append(line, '\n'))
	cl.writeMu.Unlock()
	if err != nil {
		cl.forget(id)
		return nil, err
	}

	timer := time.NewTimer(callTimeout)
	defer timer.Stop()
	select {
	case r := <-ch:
		return r.result, r.err
	case <-timer.C:
		cl.forget(id)
		return nil, fmt.Errorf("daemon did not answer %s in time", method)
	}
}

func (cl *client) forget(id int64) {
	cl.mu.Lock()
	delete(cl.pending, id)
	cl.mu.Unlock()
}

// close fails every waiting call and closes the socket.
func (cl *client) close(reason error) {
	cl.mu.Lock()
	if cl.closed {
		cl.mu.Unlock()
		return
	}
	cl.closed = true
	pending := cl.pending
	cl.pending = map[int64]chan reply{}
	cl.mu.Unlock()
	cl.conn.Close()
	for _, ch := range pending {
		ch <- reply{err: reason}
	}
}

type message struct {
	ID     *int64          `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (cl *client) readLoop() {
	r := bufio.NewReaderSize(cl.conn, 64*1024)
	for {
		line, err := readLine(r)
		if err != nil {
			break
		}
		var m message
		if json.Unmarshal(line, &m) != nil {
			continue
		}
		if m.ID == nil {
			if m.Method != "" && cl.onNotification != nil {
				cl.onNotification(m.Method, m.Params)
			}
			continue
		}
		cl.mu.Lock()
		ch := cl.pending[*m.ID]
		delete(cl.pending, *m.ID)
		cl.mu.Unlock()
		if ch == nil {
			continue
		}
		if m.Error != nil {
			ch <- reply{err: errors.New(m.Error.Message)}
		} else {
			ch <- reply{result: m.Result}
		}
	}
	cl.close(errors.New("daemon connection closed"))
	if cl.onClose != nil {
		cl.onClose()
	}
}

func readLine(r *bufio.Reader) ([]byte, error) {
	var line []byte
	for {
		chunk, err := r.ReadSlice('\n')
		line = append(line, chunk...)
		if len(line) > maxLine {
			return nil, errors.New("daemon message too large")
		}
		if err == nil {
			return line, nil
		}
		if !errors.Is(err, bufio.ErrBufferFull) {
			if err == io.EOF && len(line) == 0 {
				return nil, io.EOF
			}
			return nil, err
		}
	}
}
