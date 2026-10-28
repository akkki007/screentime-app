package planner

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/url"
	"strings"
	"time"
)

// maxResponseBytes caps what is read from a model server: a few hundred
// tokens of JSON come to a few KB, so this only stops a misbehaving endpoint.
const maxResponseBytes = 1 << 20

// ParseEndpoint checks a remote base URL. Plain http is accepted only for a
// loopback host (Ollama and friends on this machine); everything else must be
// https. Credentials in the URL are refused: the key goes in its own file.
func ParseEndpoint(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, errors.New("no endpoint set")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.Hostname() == "" {
		return nil, errors.New("the endpoint is not a valid URL")
	}
	if u.User != nil {
		return nil, errors.New("the endpoint must not contain a username or password; set the API key separately")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("the endpoint must not contain a query or fragment")
	}
	switch u.Scheme {
	case "https":
	case "http":
		if !isLoopbackHost(u.Hostname()) {
			return nil, errors.New("the endpoint must use https unless it is on this machine (localhost or a loopback address)")
		}
	default:
		return nil, errors.New("the endpoint must be an http(s) URL")
	}
	return u, nil
}

func isLoopbackHost(h string) bool {
	h = strings.TrimSuffix(strings.ToLower(h), ".")
	if h == "localhost" {
		return true
	}
	ip := net.ParseIP(h)
	return ip != nil && ip.IsLoopback()
}

// chatRequest is what the prompt builders produce.
type chatRequest struct {
	system, user string
	maxTokens    int
	schemaName   string
	schema       map[string]any
}

type wireMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type wireFormat struct {
	Type       string      `json:"type"`
	JSONSchema *wireSchema `json:"json_schema,omitempty"`
}

type wireSchema struct {
	Name   string         `json:"name"`
	Strict bool           `json:"strict"`
	Schema map[string]any `json:"schema"`
}

type wireRequest struct {
	Model          string        `json:"model"`
	Messages       []wireMessage `json:"messages"`
	Temperature    float64       `json:"temperature"` // always sent: 0
	MaxTokens      int           `json:"max_tokens"`
	Stream         bool          `json:"stream"`
	ResponseFormat *wireFormat   `json:"response_format,omitempty"`
}

// chat sends req to an OpenAI-compatible server and returns the model's
// text. It asks for constrained JSON (json_schema) first; an endpoint that
// refuses that with a 4xx is asked again with json_object, then plain, since
// the answer is validated either way.
func (p *Planner) chat(ctx context.Context, base *url.URL, key, model string, req chatRequest) (string, error) {
	var lastErr error
	for _, mode := range []string{"json_schema", "json_object", ""} {
		body := wireRequest{
			Model: model,
			Messages: []wireMessage{
				{Role: "system", Content: req.system},
				{Role: "user", Content: req.user},
			},
			MaxTokens: req.maxTokens,
		}
		switch mode {
		case "json_schema":
			body.ResponseFormat = &wireFormat{Type: "json_schema",
				JSONSchema: &wireSchema{Name: req.schemaName, Strict: true, Schema: req.schema}}
		case "json_object":
			body.ResponseFormat = &wireFormat{Type: "json_object"}
		}
		content, err := p.postOnce(ctx, base, key, body)
		var se *statusError
		if errors.As(err, &se) && (se.status == 400 || se.status == 422) && mode != "" {
			lastErr = err
			continue
		}
		return content, err
	}
	return "", lastErr
}

func (p *Planner) postOnce(ctx context.Context, base *url.URL, key string, body wireRequest) (string, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	target := strings.TrimRight(base.String(), "/")
	if !strings.HasSuffix(target, "/chat/completions") {
		target += "/chat/completions"
	}
	headers := [][2]string{
		{"Content-Type", "application/json"},
		{"Accept", "application/json"},
		{"User-Agent", "screentimed"},
	}
	if key != "" {
		headers = append(headers, [2]string{"Authorization", "Bearer " + key})
	}
	p.mu.Lock()
	roots := p.tlsRoot
	p.mu.Unlock()
	u, err := url.Parse(target)
	if err != nil {
		return "", err
	}
	resp, err := roundTrip(ctx, "POST", u, headers, payload, p.tm, roots)
	if err != nil {
		return "", err
	}
	if resp.status < 200 || resp.status > 299 {
		return "", &statusError{resp.status}
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(resp.body, &out); err != nil {
		return "", errors.New("the endpoint's answer was not a chat completion")
	}
	if len(out.Choices) == 0 || strings.TrimSpace(out.Choices[0].Message.Content) == "" {
		return "", errors.New("the model returned no answer")
	}
	return out.Choices[0].Message.Content, nil
}

// complete runs req on the configured provider.
func (p *Planner) complete(ctx context.Context, req chatRequest) (string, error) {
	o := p.options()
	if o.Provider == "remote" {
		base, err := ParseEndpoint(o.Endpoint)
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(o.Model) == "" {
			return "", errors.New("no model name set for the remote provider")
		}
		return p.chat(ctx, base, ReadKey(p.cfg.KeyPath), strings.TrimSpace(o.Model), req)
	}
	return p.completeLocal(ctx, o, req)
}

// completeLocal runs req on the sidecar, restarting it once if it turns out
// to have died (a crash, or the machine was suspended).
func (p *Planner) completeLocal(ctx context.Context, o Options, req chatRequest) (string, error) {
	sup := p.supervisor(o)
	var lastErr error
	for range 2 {
		lease, err := sup.Acquire(ctx)
		if err != nil {
			return "", err
		}
		content, err := p.chat(ctx, lease.BaseURL, lease.Key, "local", req)
		lease.Release()
		var ce *connError
		if errors.As(err, &ce) && ctx.Err() == nil {
			lastErr = err
			sup.Invalidate(lease)
			continue
		}
		return content, err
	}
	return "", lastErr
}

// supervisor returns the sidecar supervisor, creating it on first use.
func (p *Planner) supervisor(o Options) *Supervisor {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.sup == nil {
		idle := p.tm.idle
		if idle == 0 {
			idle = time.Duration(o.IdleMinutes) * time.Minute
		}
		p.sup = NewSupervisor(SupervisorConfig{
			ServerPath:   o.ServerPath,
			ModelPath:    o.ModelPath,
			RunDir:       p.cfg.RunDir,
			Idle:         idle,
			StartTimeout: p.tm.start,
			TermGrace:    p.tm.grace,
			Dial:         p.tm.dial,
			Response:     p.tm.response,
		})
	}
	return p.sup
}
