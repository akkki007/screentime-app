package tlsx

import (
	"bytes"
	"crypto/x509"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/akkki007/screentime-app/internal/httpx"
)

func serveOne(t *testing.T, req httpx.Request, roots *x509.CertPool) httpx.Reply {
	t.Helper()
	in, _ := json.Marshal(req)
	var out bytes.Buffer
	if err := Serve(bytes.NewReader(in), &out, roots); err != nil {
		t.Fatal(err)
	}
	var reply httpx.Reply
	if err := json.Unmarshal(out.Bytes(), &reply); err != nil {
		t.Fatal(err)
	}
	return reply
}

func TestServeFetchesHTTPSAndVerifiesTheCertificate(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer k" {
			w.WriteHeader(401)
			return
		}
		w.Write([]byte(`{"ok":true}`))
	}))
	defer ts.Close()
	req := httpx.Request{Method: "POST", URL: ts.URL + "/v1/chat/completions", Headers: [][2]string{{"Authorization", "Bearer k"}}, Body: []byte("{}")}

	if r := serveOne(t, req, nil); r.Error == "" || !r.Transport {
		t.Fatalf("an untrusted certificate was accepted: %+v", r)
	}
	pool := x509.NewCertPool()
	pool.AddCert(ts.Certificate())
	r := serveOne(t, req, pool)
	if r.Response == nil || r.Response.Status != 200 || string(r.Response.Body) != `{"ok":true}` {
		t.Fatalf("reply = %+v", r)
	}
}

func TestServeRefusesWhatItIsNotFor(t *testing.T) {
	for _, req := range []httpx.Request{
		{Method: "POST", URL: "http://127.0.0.1:1/v1"},        // plain http never needs the helper
		{Method: "POST", URL: "file:///etc/passwd"},           // not a web URL
		{Method: "DELETE", URL: "https://example.com/v1"},     // not a planner method
		{Method: "POST", URL: "https://user:pw@example.com/"}, // credentials in the URL
	} {
		if r := serveOne(t, req, nil); r.Error == "" || r.Response != nil {
			t.Errorf("%s %s: reply = %+v", req.Method, req.URL, r)
		}
	}
	var out bytes.Buffer
	if err := Serve(strings.NewReader("not json"), &out, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "unreadable request") {
		t.Errorf("out = %s", out.String())
	}
}
