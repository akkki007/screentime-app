// Package tlsx is the TLS half of httpx. Only the screentime-fetch helper
// (and tests) import it, so the daemon itself never links crypto/tls.
package tlsx

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/url"
	"time"

	"github.com/akkki007/screentime-app/internal/httpx"
)

// Upgrade returns an httpx.Upgrade that verifies the server against roots
// (nil: the system's roots).
func Upgrade(roots *x509.CertPool) httpx.Upgrade {
	return func(ctx context.Context, conn net.Conn, host string, timeout time.Duration) (net.Conn, error) {
		tc := tls.Client(conn, &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12, RootCAs: roots})
		hctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		if err := tc.HandshakeContext(hctx); err != nil {
			return nil, err
		}
		return tc, nil
	}
}

// requestTimeout bounds the helper's whole life, whatever the daemon does.
const requestTimeout = 3 * time.Minute

// Serve answers one httpx.Request from in on out. It only speaks https:
// plain http never needs the helper.
func Serve(in io.Reader, out io.Writer, roots *x509.CertPool) error {
	reply := serve(in, roots)
	return json.NewEncoder(out).Encode(reply)
}

func serve(in io.Reader, roots *x509.CertPool) httpx.Reply {
	req, err := httpx.ReadRequest(in)
	if err != nil {
		return httpx.Reply{Error: "unreadable request"}
	}
	u, err := url.Parse(req.URL)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil {
		return httpx.Reply{Error: "the helper only fetches https URLs"}
	}
	if req.Method != "POST" && req.Method != "GET" {
		return httpx.Reply{Error: "unsupported method"}
	}
	dial := time.Duration(req.DialMs) * time.Millisecond
	if dial <= 0 || dial > time.Minute {
		dial = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()
	res, err := httpx.RoundTrip(ctx, req.Method, u, req.Headers, req.Body, dial, Upgrade(roots))
	if err != nil {
		var ce *httpx.ConnError
		return httpx.Reply{Error: err.Error(), Transport: errors.As(err, &ce)}
	}
	return httpx.Reply{Response: res}
}
