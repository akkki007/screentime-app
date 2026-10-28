package planner

import (
	"crypto/x509"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/akkki007/screentime-app/internal/apps"
	"github.com/akkki007/screentime-app/internal/httpx/tlsx"
	"github.com/akkki007/screentime-app/internal/store"
)

// TestMain doubles as a tiny fake llama-server: when FAKE_LLAMA_SERVER=1 the
// test binary, started with llama-server's flags, serves /health and
// /v1/chat/completions instead of running tests. No real model is involved.
//
// With FAKE_FETCH=1 it is the screentime-fetch helper instead, trusting the
// PEM roots in FAKE_FETCH_CA (or the system's when unset).
func TestMain(m *testing.M) {
	if os.Getenv("FAKE_FETCH") == "1" {
		var roots *x509.CertPool
		if ca := os.Getenv("FAKE_FETCH_CA"); ca != "" {
			pem, err := os.ReadFile(ca)
			if err != nil {
				os.Exit(2)
			}
			roots = x509.NewCertPool()
			roots.AppendCertsFromPEM(pem)
		}
		if err := tlsx.Serve(os.Stdin, os.Stdout, roots); err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}
	if os.Getenv("FAKE_LLAMA_SERVER") == "1" {
		fakeLlama(os.Args[1:])
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// Environment knobs for the fake, all optional:
//
//	FAKE_LLAMA_LOG    append "start <pid>" per start (and the args to <file>.args)
//	FAKE_LLAMA_HELP   "full" (default), "env" (only LLAMA_API_KEY), "none" (no key support)
//	FAKE_LLAMA_CRASH  path: the first chat request creates it and kills the server
//	FAKE_LLAMA_LOAD   milliseconds /health answers 503 after start
func fakeLlama(args []string) {
	help := os.Getenv("FAKE_LLAMA_HELP")
	for _, a := range args {
		if a == "--help" {
			fmt.Println("usage: llama-server [options]")
			fmt.Println("  --model FNAME")
			fmt.Println("  --no-webui")
			fmt.Println("  --offline")
			switch help {
			case "env":
				fmt.Println("  --api-key KEY   (env: LLAMA_API_KEY)")
			case "none":
			default:
				fmt.Println("  --api-key-file FNAME")
			}
			return
		}
	}
	var port, keyFile string
	for i := 0; i < len(args)-1; i++ {
		switch args[i] {
		case "--port":
			port = args[i+1]
		case "--api-key-file":
			keyFile = args[i+1]
		}
	}
	key := os.Getenv("LLAMA_API_KEY")
	if keyFile != "" {
		b, err := os.ReadFile(keyFile)
		if err != nil {
			os.Exit(3)
		}
		key = strings.TrimSpace(string(b))
	}
	if path := os.Getenv("FAKE_LLAMA_LOG"); path != "" {
		if f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
			fmt.Fprintf(f, "start %d\n", os.Getpid())
			f.Close()
		}
		os.WriteFile(path+".args", []byte(strings.Join(args, "\n")), 0o600)
	}
	loadedAt := time.Now()
	if ms := os.Getenv("FAKE_LLAMA_LOAD"); ms != "" {
		var n int
		fmt.Sscan(ms, &n)
		loadedAt = loadedAt.Add(time.Duration(n) * time.Millisecond)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if time.Now().Before(loadedAt) {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		if key != "" && r.Header.Get("Authorization") != "Bearer "+key {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if crash := os.Getenv("FAKE_LLAMA_CRASH"); crash != "" {
			if _, err := os.Stat(crash); err != nil {
				os.WriteFile(crash, nil, 0o600)
				os.Exit(1)
			}
		}
		body, _ := io.ReadAll(r.Body)
		writeChat(w, cannedAnswer(body))
	})
	ln, err := net.Listen("tcp", "127.0.0.1:"+port)
	if err != nil {
		os.Exit(4)
	}
	http.Serve(ln, mux)
}

func writeChat(w http.ResponseWriter, content string) {
	json.NewEncoder(w).Encode(map[string]any{
		"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "content": content}}},
	})
}

// cannedAnswer answers a request the way a model constrained to its schema
// would: valid, and drawn from the schema's own enums.
func cannedAnswer(body []byte) string {
	var req struct {
		ResponseFormat struct {
			JSONSchema struct {
				Name   string         `json:"name"`
				Schema map[string]any `json:"schema"`
			} `json:"json_schema"`
		} `json:"response_format"`
	}
	json.Unmarshal(body, &req)
	if req.ResponseFormat.JSONSchema.Name == "app_categories" {
		item := dig(req.ResponseFormat.JSONSchema.Schema, "properties", "results", "items", "properties")
		ids, _ := dig(item, "appId")["enum"].([]any)
		cats, _ := dig(item, "category")["enum"].([]any)
		type result struct {
			AppID      string  `json:"appId"`
			Category   string  `json:"category"`
			Confidence float64 `json:"confidence"`
		}
		var out []result
		for _, id := range ids {
			for _, c := range cats {
				if c != "Other" {
					out = append(out, result{id.(string), c.(string), 0.9})
					break
				}
			}
		}
		b, _ := json.Marshal(map[string]any{"results": out})
		return string(b)
	}
	return `{"summary":"A calm day.","suggestions":[{"kind":"focus","appId":null,"category":null,"minutes":25,"reason":"One short session helps."}]}`
}

func dig(m map[string]any, path ...string) map[string]any {
	for _, k := range path {
		m, _ = m[k].(map[string]any)
	}
	return m
}

// --- shared fixtures ------------------------------------------------------

const fixedNow = 1_790_762_400_000 // 2026-09-30 10:00 UTC

func openDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func mustExec(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.Exec(q, args...); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
}

// addApp inserts an app (category by name, "" for none) and one hour of
// usage ending just before "now".
func addApp(t *testing.T, db *sql.DB, appID, name, category, source, title string) {
	t.Helper()
	mustExec(t, db, `INSERT INTO apps (app_id, name, category_id, category_source)
		VALUES (?, ?, (SELECT id FROM categories WHERE name = ?), ?)`, appID, name, category, source)
	mustExec(t, db, `INSERT INTO sessions (app_id, title, start_ts, end_ts, source)
		VALUES ((SELECT id FROM apps WHERE app_id = ?), ?, ?, ?, 'desktop')`,
		appID, title, int64(fixedNow-3_600_000), int64(fixedNow-60_000))
}

func categoryOf(t *testing.T, db *sql.DB, appID string) (name, source string) {
	t.Helper()
	var n sql.NullString
	if err := db.QueryRow(`SELECT categories.name, apps.category_source FROM apps
		LEFT JOIN categories ON categories.id = apps.category_id WHERE app_id = ?`, appID).Scan(&n, &source); err != nil {
		t.Fatal(err)
	}
	return n.String, source
}

func categoryID(t *testing.T, db *sql.DB, name string) int64 {
	t.Helper()
	var id int64
	if err := db.QueryRow("SELECT id FROM categories WHERE name = ?", name).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

type httpServer struct {
	URL string
	srv *http.Server

	mu       sync.Mutex
	requests []recorded
}

type recorded struct {
	Auth string
	Body string
}

func (s *httpServer) all() []recorded {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]recorded(nil), s.requests...)
}

func (s *httpServer) record(r *http.Request) recorded {
	b, _ := io.ReadAll(r.Body)
	rec := recorded{Auth: r.Header.Get("Authorization"), Body: string(b)}
	s.mu.Lock()
	s.requests = append(s.requests, rec)
	s.mu.Unlock()
	return rec
}

// newEndpoint serves /v1/chat/completions with handler, recording each request.
func newEndpoint(t *testing.T, handler func(w http.ResponseWriter, rec recorded)) *httpServer {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &httpServer{URL: "http://" + ln.Addr().String() + "/v1"}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, r *http.Request) {
		handler(w, s.record(r))
	})
	s.srv = &http.Server{Handler: mux}
	go s.srv.Serve(ln)
	t.Cleanup(func() { s.srv.Close() })
	return s
}

func answer(content string) func(http.ResponseWriter, recorded) {
	return func(w http.ResponseWriter, rec recorded) { writeChat(w, content) }
}

func noLookup(string) (apps.Entry, bool) { return apps.Entry{}, false }

// remotePlanner is a planner using the remote provider on url.
func remotePlanner(t *testing.T, db *sql.DB, url string) *Planner {
	t.Helper()
	p := New(Config{
		DB:      db,
		Now:     func() int64 { return fixedNow },
		Lookup:  noLookup,
		KeyPath: filepath.Join(t.TempDir(), "sub", "planner.key"),
		RunDir:  t.TempDir(),
		// This test binary, as the helper, when FAKE_FETCH=1 is set.
		FetchPath: os.Args[0],
		Options:   Options{Provider: "remote", Endpoint: url, Model: "test-model"},
	})
	t.Cleanup(p.Close)
	return p
}

// localPlanner uses this test binary as a fake llama-server.
func localPlanner(t *testing.T, db *sql.DB, idle time.Duration) (*Planner, string) {
	t.Helper()
	t.Setenv("FAKE_LLAMA_SERVER", "1")
	log := filepath.Join(t.TempDir(), "starts.log")
	t.Setenv("FAKE_LLAMA_LOG", log)
	model := filepath.Join(t.TempDir(), "model.gguf")
	if err := os.WriteFile(model, []byte("not a real model"), 0o600); err != nil {
		t.Fatal(err)
	}
	p := New(Config{
		DB:      db,
		Now:     func() int64 { return fixedNow },
		Lookup:  noLookup,
		KeyPath: filepath.Join(t.TempDir(), "planner.key"),
		RunDir:  filepath.Join(t.TempDir(), "run"),
		Options: Options{Provider: "local", ServerPath: os.Args[0], ModelPath: model, IdleMinutes: 5},
	})
	p.tm.idle = idle
	p.tm.start = 20 * time.Second
	p.tm.grace = 2 * time.Second
	t.Cleanup(p.Close)
	return p, log
}

func countStarts(t *testing.T, log string) int {
	t.Helper()
	b, _ := os.ReadFile(log)
	return strings.Count(string(b), "start ")
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}
