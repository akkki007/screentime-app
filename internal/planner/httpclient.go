package planner

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/akkki007/screentime-app/internal/httpx"
)

// roundTrip sends one request to a model server. Plain http (loopback only:
// the local sidecar, or Ollama and friends) goes straight from the daemon;
// https goes through the screentime-fetch helper, so the daemon never links
// TLS (ADR 8, amendments).
func (p *Planner) roundTrip(ctx context.Context, method string, u *url.URL, headers [][2]string, body []byte, dial time.Duration) (*httpx.Response, error) {
	if u.Scheme == "https" {
		return httpx.Call(ctx, p.fetchPath(), httpx.Request{Method: method, URL: u.String(), Headers: headers, Body: body, DialMs: dial.Milliseconds()})
	}
	return httpx.RoundTrip(ctx, method, u, headers, body, dial, nil)
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
