// Package httpx is the small HTTP/1.1 client behind the optional planner
// (ADR 8). It has no TLS: the always-on daemon must not link crypto/tls or
// net/http (about 4 MB of binary and 3 MB of resident pages, measured), so a
// user who never turns the planner on pays nothing for it. An https request
// goes through Call, which runs the separate screentime-fetch helper
// (package tlsx) for that one request.
package httpx

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/textproto"
	"net/url"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// The needs are narrow: one JSON POST or GET per connection, a body of at
// most MaxResponseBytes, no redirects, no cookies, no HTTP/2, no compression
// (none is requested). Because it speaks only HTTP/1.1 directly, proxies are
// not supported.

// MaxResponseBytes caps what is read from a model server: a few hundred
// tokens of JSON come to a few KB, so this only stops a misbehaving endpoint.
const MaxResponseBytes = 1 << 20

// Response is a status and a body; the headers are not kept.
type Response struct {
	Status int    `json:"status"`
	Body   []byte `json:"body"`
}

// Upgrade wraps a connected socket in TLS for host, within timeout.
type Upgrade func(ctx context.Context, conn net.Conn, host string, timeout time.Duration) (net.Conn, error)

// LoopbackOnly is a Dialer.Control that refuses any peer that is not a
// loopback address, so a name like "localhost" cannot be made to reach out.
func LoopbackOnly(network, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
		return errors.New("refusing a non-loopback address for a plain http endpoint")
	}
	return nil
}

// RoundTrip sends one request on a fresh connection and reads the answer.
// ctx bounds the whole exchange; dial bounds connecting and the TLS
// handshake, which up performs (nil: https is refused). Redirects are never followed: a 3xx comes back as an ordinary
// response for the caller to refuse, because a redirect could carry the API
// key to another host.
func RoundTrip(ctx context.Context, method string, u *url.URL, headers [][2]string, body []byte, dial time.Duration, up Upgrade) (*Response, error) {
	host, port := u.Hostname(), u.Port()
	if port == "" {
		port = map[string]string{"http": "80", "https": "443"}[u.Scheme]
	}
	d := &net.Dialer{Timeout: dial}
	if u.Scheme == "http" {
		d.Control = LoopbackOnly
	}
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(host, port))
	if err != nil {
		return nil, Transport(err)
	}
	defer conn.Close()
	// Closing the connection is what interrupts a blocked read when ctx ends.
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	defer stop()
	if dl, ok := ctx.Deadline(); ok {
		conn.SetDeadline(dl)
	}

	var rw io.ReadWriter = conn
	if u.Scheme == "https" {
		if up == nil {
			return nil, errors.New("https needs the screentime-fetch helper")
		}
		tc, err := up(ctx, conn, host, dial)
		if err != nil {
			return nil, Transport(err)
		}
		rw = tc
	} else if u.Scheme != "http" {
		return nil, fmt.Errorf("unsupported scheme %q", u.Scheme)
	}

	var req strings.Builder
	uri := u.RequestURI()
	if !strings.HasPrefix(uri, "/") {
		uri = "/" + uri
	}
	req.WriteString(method + " " + uri + " HTTP/1.1\r\n")
	req.WriteString("Host: " + u.Host + "\r\nConnection: close\r\n")
	for _, h := range headers {
		if strings.ContainsAny(h[0]+h[1], "\r\n") {
			return nil, errors.New("a request header contains a line break")
		}
		req.WriteString(h[0] + ": " + h[1] + "\r\n")
	}
	if body != nil {
		req.WriteString("Content-Length: " + strconv.Itoa(len(body)) + "\r\n")
	}
	req.WriteString("\r\n")
	if _, err := io.WriteString(rw, req.String()); err != nil {
		return nil, Transport(err)
	}
	if body != nil {
		if _, err := rw.Write(body); err != nil {
			return nil, Transport(err)
		}
	}
	res, err := readResponse(bufio.NewReader(rw))
	if err != nil && ctx.Err() != nil {
		err = Transport(ctx.Err()) // the connection was closed under it
	}
	return res, err
}

var errNotHTTP = errors.New("the server did not answer with HTTP")

func readResponse(br *bufio.Reader) (*Response, error) {
	tp := textproto.NewReader(br)
	line, err := tp.ReadLine()
	if err != nil {
		return nil, Transport(err) // closed without answering
	}
	// "HTTP/1.1 200 OK"
	f := strings.Fields(line)
	if len(f) < 2 || !strings.HasPrefix(f[0], "HTTP/1.") {
		return nil, errNotHTTP
	}
	status, err := strconv.Atoi(f[1])
	if err != nil || status < 100 || status > 599 {
		return nil, errNotHTTP
	}
	hdr, err := tp.ReadMIMEHeader()
	if err != nil {
		return nil, Transport(err)
	}
	res := &Response{Status: status}
	if status < 200 || status == 204 || status == 304 || (status >= 300 && status < 400) {
		return res, nil // nothing the caller reads
	}
	if enc := hdr.Get("Content-Encoding"); enc != "" && enc != "identity" {
		return nil, errors.New("the server compressed its answer, which was not requested")
	}
	switch {
	case strings.EqualFold(hdr.Get("Transfer-Encoding"), "chunked"):
		res.Body, err = readChunked(tp)
	case hdr.Get("Content-Length") != "":
		n, perr := strconv.ParseInt(hdr.Get("Content-Length"), 10, 64)
		if perr != nil || n < 0 {
			return nil, errNotHTTP
		}
		if n > MaxResponseBytes {
			return nil, errTooLarge
		}
		res.Body = make([]byte, n)
		_, err = io.ReadFull(br, res.Body)
	default: // until the server closes, as Connection: close allows
		res.Body, err = io.ReadAll(io.LimitReader(br, MaxResponseBytes+1))
		if err == nil && len(res.Body) > MaxResponseBytes {
			return nil, errTooLarge
		}
	}
	if err != nil {
		if errors.Is(err, errTooLarge) {
			return nil, err
		}
		return nil, Transport(err)
	}
	return res, nil
}

var errTooLarge = errors.New("the endpoint's answer is too large")

func readChunked(tp *textproto.Reader) ([]byte, error) {
	var out []byte
	for {
		line, err := tp.ReadLine()
		if err != nil {
			return nil, err
		}
		sizeText, _, _ := strings.Cut(line, ";") // chunk extensions are ignored
		size, err := strconv.ParseUint(strings.TrimSpace(sizeText), 16, 32)
		if err != nil {
			return nil, errNotHTTP
		}
		if size == 0 {
			for { // trailers, up to the blank line
				t, err := tp.ReadLine()
				if err != nil || t == "" {
					return out, err
				}
			}
		}
		if uint64(len(out))+size > MaxResponseBytes {
			return nil, errTooLarge
		}
		chunk := make([]byte, size)
		if _, err := io.ReadFull(tp.R, chunk); err != nil {
			return nil, err
		}
		out = append(out, chunk...)
		if tail, err := tp.ReadLine(); err != nil || tail != "" {
			if err == nil {
				err = errNotHTTP
			}
			return nil, err
		}
	}
}

// ConnError is a failure to talk to the server at all, as opposed to the
// server answering badly. The local provider retries these once.
type ConnError struct{ Err error }

func (e *ConnError) Error() string { return e.Err.Error() }
func (e *ConnError) Unwrap() error { return e.Err }

// Transport classifies an I/O failure. Its text carries no URL and no
// header, so it cannot leak the API key.
func Transport(err error) error {
	var ce *ConnError
	if errors.As(err, &ce) {
		return err
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return &ConnError{errors.New("the request timed out")}
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return &ConnError{errors.New("the request timed out")}
	}
	if errors.Is(err, syscall.EAFNOSUPPORT) {
		return &ConnError{errors.New("network access is blocked for the tracker's service; turn the planner off and on again to grant it")}
	}
	if errors.Is(err, context.Canceled) {
		return &ConnError{errors.New("the request was cancelled")}
	}
	return &ConnError{fmt.Errorf("could not reach the model server: %w", err)}
}
