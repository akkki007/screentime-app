package httpx

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os/exec"
)

// Request is one https request for the screentime-fetch helper, which reads
// it as JSON on stdin. The API key travels in Headers, so it is never on a
// command line or in an environment another process can read.
type Request struct {
	Method  string      `json:"method"`
	URL     string      `json:"url"`
	Headers [][2]string `json:"headers"`
	Body    []byte      `json:"body"`
	DialMs  int64       `json:"dialMs"`
}

// Reply is the helper's answer on stdout: a response, or an error whose text
// carries no URL or header.
type Reply struct {
	Response  *Response `json:"response,omitempty"`
	Error     string    `json:"error,omitempty"`
	Transport bool      `json:"transport,omitempty"`
}

// maxRequestBytes bounds what the helper reads: prompts are aggregates, a
// few KB.
const maxRequestBytes = 1 << 20

// Call runs the helper at exe for one request. The helper is killed when ctx
// ends. Its stderr is discarded, because nothing about a request is logged.
func Call(ctx context.Context, exe string, req Request) (*Response, error) {
	if exe == "" {
		return nil, errors.New("the remote planner needs the screentime-fetch helper next to screentimed")
	}
	in, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	cmd := exec.CommandContext(ctx, exe)
	cmd.Stdin = bytes.NewReader(in)
	cmd.Stdout = &limitedWriter{w: &out, n: 2*MaxResponseBytes + 4096} // base64 and the envelope
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, Transport(ctx.Err())
		}
		if out.Len() == 0 {
			return nil, &ConnError{errors.New("the screentime-fetch helper failed")}
		}
	}
	var reply Reply
	if err := json.Unmarshal(out.Bytes(), &reply); err != nil {
		return nil, &ConnError{errors.New("the screentime-fetch helper gave an unreadable answer")}
	}
	switch {
	case reply.Error != "" && reply.Transport:
		return nil, &ConnError{errors.New(reply.Error)}
	case reply.Error != "":
		return nil, errors.New(reply.Error)
	case reply.Response == nil:
		return nil, &ConnError{errors.New("the screentime-fetch helper gave no answer")}
	}
	return reply.Response, nil
}

// ReadRequest is the helper's side of Call.
func ReadRequest(r io.Reader) (Request, error) {
	var req Request
	b, err := io.ReadAll(io.LimitReader(r, maxRequestBytes+1))
	if err != nil {
		return req, err
	}
	if len(b) > maxRequestBytes {
		return req, errors.New("request too large")
	}
	return req, json.Unmarshal(b, &req)
}

type limitedWriter struct {
	w io.Writer
	n int
}

func (l *limitedWriter) Write(p []byte) (int, error) {
	if len(p) > l.n {
		return 0, errors.New("helper answer too large")
	}
	l.n -= len(p)
	return l.w.Write(p)
}
