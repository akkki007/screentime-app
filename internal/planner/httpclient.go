package planner

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/textproto"
	"net/url"
	"strconv"
	"strings"
	"syscall"
)

// A deliberately small HTTP/1.1 client. The daemon does not link net/http
// (about 4 MB of binary and 3 MB of resident file pages on a 15 MB / 11 MB
// daemon, measured), so that a user who never turns the planner on pays
// nothing for it. The needs are narrow: one JSON POST or GET per connection,
// a body of at most maxResponseBytes, no redirects, no cookies, no HTTP/2,
// no compression (none is requested). Because it speaks only HTTP/1.1
// directly, proxies are not supported.

type rootPool = *x509.CertPool

type response struct {
	status int
	body   []byte
}

// loopbackOnly is a Dialer.Control that refuses any peer that is not a
// loopback address, so a name like "localhost" cannot be made to reach out.
func loopbackOnly(network, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
		return errors.New("refusing a non-loopback address for a plain http endpoint")
	}
	return nil
}

// roundTrip sends one request on a fresh connection and reads the answer.
// ctx bounds the whole exchange; tm.dial bounds connecting and the TLS
// handshake. Redirects are never followed: a 3xx comes back as an ordinary
// response for the caller to refuse, because a redirect could carry the API
// key to another host.
func roundTrip(ctx context.Context, method string, u *url.URL, headers [][2]string, body []byte, tm timeouts, roots rootPool) (*response, error) {
	host, port := u.Hostname(), u.Port()
	if port == "" {
		port = map[string]string{"http": "80", "https": "443"}[u.Scheme]
	}
	d := &net.Dialer{Timeout: tm.dial}
	if u.Scheme == "http" {
		d.Control = loopbackOnly
	}
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(host, port))
	if err != nil {
		return nil, transportError(err)
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
		tc := tls.Client(conn, &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12, RootCAs: roots})
		hctx, cancel := context.WithTimeout(ctx, tm.dial)
		err := tc.HandshakeContext(hctx)
		cancel()
		if err != nil {
			return nil, transportError(err)
		}
		rw = tc
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
		return nil, transportError(err)
	}
	if body != nil {
		if _, err := rw.Write(body); err != nil {
			return nil, transportError(err)
		}
	}
	res, err := readResponse(bufio.NewReader(rw))
	if err != nil && ctx.Err() != nil {
		err = transportError(ctx.Err()) // the connection was closed under it
	}
	return res, err
}

var errNotHTTP = errors.New("the server did not answer with HTTP")

func readResponse(br *bufio.Reader) (*response, error) {
	tp := textproto.NewReader(br)
	line, err := tp.ReadLine()
	if err != nil {
		return nil, transportError(err) // closed without answering
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
		return nil, transportError(err)
	}
	res := &response{status: status}
	if status < 200 || status == 204 || status == 304 || (status >= 300 && status < 400) {
		return res, nil // nothing the caller reads
	}
	if enc := hdr.Get("Content-Encoding"); enc != "" && enc != "identity" {
		return nil, errors.New("the server compressed its answer, which was not requested")
	}
	switch {
	case strings.EqualFold(hdr.Get("Transfer-Encoding"), "chunked"):
		res.body, err = readChunked(tp)
	case hdr.Get("Content-Length") != "":
		n, perr := strconv.ParseInt(hdr.Get("Content-Length"), 10, 64)
		if perr != nil || n < 0 {
			return nil, errNotHTTP
		}
		if n > maxResponseBytes {
			return nil, errTooLarge
		}
		res.body = make([]byte, n)
		_, err = io.ReadFull(br, res.body)
	default: // until the server closes, as Connection: close allows
		res.body, err = io.ReadAll(io.LimitReader(br, maxResponseBytes+1))
		if err == nil && len(res.body) > maxResponseBytes {
			return nil, errTooLarge
		}
	}
	if err != nil {
		if errors.Is(err, errTooLarge) {
			return nil, err
		}
		return nil, transportError(err)
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
		if uint64(len(out))+size > maxResponseBytes {
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

// connError is a failure to talk to the server at all, as opposed to the
// server answering badly. The local provider retries these once.
type connError struct{ err error }

func (e *connError) Error() string { return e.err.Error() }
func (e *connError) Unwrap() error { return e.err }

// transportError classifies an I/O failure. Its text carries no URL and no
// header, so it cannot leak the API key.
func transportError(err error) error {
	if errors.Is(err, context.DeadlineExceeded) {
		return &connError{errors.New("the request timed out")}
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return &connError{errors.New("the request timed out")}
	}
	if errors.Is(err, context.Canceled) {
		return &connError{errors.New("the request was cancelled")}
	}
	return &connError{fmt.Errorf("could not reach the model server: %w", err)}
}

// statusError is a non-2xx answer. Its text never includes the response
// body: providers sometimes echo part of the key there.
type statusError struct{ status int }

func (e *statusError) Error() string {
	switch {
	case e.status == 401 || e.status == 403:
		return fmt.Sprintf("the endpoint rejected the API key (HTTP %d)", e.status)
	case e.status == 404:
		return "the endpoint or model was not found (HTTP 404)"
	case e.status == 429:
		return "the endpoint is rate limiting requests (HTTP 429)"
	case e.status >= 300 && e.status < 400:
		return fmt.Sprintf("the endpoint answered with a redirect (HTTP %d); redirects are not followed", e.status)
	}
	return fmt.Sprintf("the endpoint answered HTTP %d", e.status)
}
