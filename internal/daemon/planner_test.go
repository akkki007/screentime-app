package daemon

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/akkki007/screentime-app/internal/apps"
	"github.com/akkki007/screentime-app/internal/focus"
	"github.com/akkki007/screentime-app/internal/ipc"
	"github.com/akkki007/screentime-app/internal/store"
)

type noProvider struct{}

func (noProvider) ID() string                              { return "none" }
func (noProvider) Available(context.Context) bool          { return true }
func (noProvider) OnFocusChange(func(focus.Window)) func() { return func() {} }
func (noProvider) OnIdleChange(func(bool), int64) func()   { return func() {} }

type quiet struct{}

func (quiet) Notify(string, string) {}

const now = 1_790_762_400_000

func newDaemon(t *testing.T) (*Daemon, string) {
	t.Helper()
	dir := t.TempDir()
	db, err := store.Open(filepath.Join(dir, "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	d, err := New(Config{
		DB: db, Provider: noProvider{}, Notifier: quiet{},
		Now:            func() int64 { return now },
		Lookup:         func(string) (apps.Entry, bool) { return apps.Entry{}, false },
		PlannerKeyPath: filepath.Join(dir, "data", "planner.key"),
		PlannerRunDir:  filepath.Join(dir, "run"),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(d.Stop)
	return d, dir
}

// call runs a method like the RPC server does and returns the JSON result or
// the error text.
func call(t *testing.T, d *Daemon, method string, params string) (string, error) {
	t.Helper()
	var raw json.RawMessage
	if params != "" {
		raw = json.RawMessage(params)
	}
	res, err := d.Handle(method, raw)
	if err != nil {
		return "", err
	}
	b, jerr := json.Marshal(res)
	if jerr != nil {
		t.Fatal(jerr)
	}
	return string(b), nil
}

func mustCall(t *testing.T, d *Daemon, method, params string) string {
	t.Helper()
	out, err := call(t, d, method, params)
	if err != nil {
		t.Fatalf("%s: %v", method, err)
	}
	return out
}

func TestPlannerIsOffByDefaultAndCostsNothing(t *testing.T) {
	d, dir := newDaemon(t)
	if d.planner != nil {
		t.Fatal("a planner exists by default")
	}
	before := runtime.NumGoroutine() // the daemon and its database, planner aside

	// Other settings changes leave it off.
	mustCall(t, d, "settings.set", `{"captureTitles":true,"plannerProvider":"remote","plannerEndpoint":"https://api.example.com/v1"}`)
	if d.planner != nil {
		t.Error("configuring a provider switched the planner on")
	}

	got := mustCall(t, d, "planner.status", "")
	want := `{"enabled":false,"provider":"remote","state":"off","serverFound":false,"modelFound":false,"keySet":false,"host":"api.example.com","lastError":null}`
	if got != want {
		t.Errorf("status = %s\nwant     %s", got, want)
	}
	for _, m := range []string{"planner.suggest", "planner.categorize"} {
		params := ""
		if m == "planner.suggest" {
			params = `{}`
		}
		if _, err := call(t, d, m, params); err == nil || err.Error() != "planner is off" {
			t.Errorf("%s: err = %v, want \"planner is off\"", m, err)
		}
	}
	mustCall(t, d, "planner.stop", "")
	if g := runtime.NumGoroutine(); g > before {
		t.Errorf("%d goroutines before the planner calls, %d after", before, g)
	}

	// Nothing was created on disk, and no goroutine was left behind.
	if _, err := os.Stat(filepath.Join(dir, "run")); err == nil {
		t.Error("the runtime directory was created")
	}
	if _, err := os.Stat(filepath.Join(dir, "data")); err == nil {
		t.Error("the key directory was created")
	}
	var s ipc.Settings
	if err := json.Unmarshal([]byte(mustCall(t, d, "settings.get", "")), &s); err != nil {
		t.Fatal(err)
	}
	if s.PlannerEnabled || s.PlannerProvider != "remote" || s.PlannerIdleMinutes != 5 {
		t.Errorf("settings = %+v", s)
	}
}

func TestDefaultPlannerSettings(t *testing.T) {
	d, _ := newDaemon(t)
	var m map[string]any
	if err := json.Unmarshal([]byte(mustCall(t, d, "settings.get", "")), &m); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"plannerEnabled": false, "plannerProvider": "local", "plannerEndpoint": "", "plannerModel": "",
		"plannerServerPath": "", "plannerModelPath": "", "plannerIdleMinutes": float64(5),
	}
	for k, v := range want {
		if m[k] != v {
			t.Errorf("%s = %v, want %v", k, m[k], v)
		}
	}
}

func TestEnablingAndDisablingThroughSettings(t *testing.T) {
	d, _ := newDaemon(t)
	srv := fakeEndpoint(t)
	mustCall(t, d, "settings.set", `{"plannerEnabled":true,"plannerProvider":"remote","plannerEndpoint":"`+srv+`","plannerModel":"m"}`)
	if d.planner == nil {
		t.Fatal("enabling did not create the planner")
	}
	st := mustCall(t, d, "planner.status", "")
	if !strings.Contains(st, `"enabled":true`) || !strings.Contains(st, `"state":"ready"`) || !strings.Contains(st, `"host":"127.0.0.1"`) {
		t.Errorf("status = %s", st)
	}
	out := mustCall(t, d, "planner.suggest", `{"goal":"ship it"}`)
	if !strings.Contains(out, `"summary":"A plan."`) || !strings.Contains(out, `"kind":"note"`) {
		t.Errorf("suggest = %s", out)
	}

	mustCall(t, d, "settings.set", `{"plannerEnabled":false}`)
	if d.planner != nil {
		t.Fatal("disabling kept the planner")
	}
	if _, err := call(t, d, "planner.suggest", `{}`); err == nil || err.Error() != "planner is off" {
		t.Errorf("err = %v", err)
	}
	if st := mustCall(t, d, "planner.status", ""); !strings.Contains(st, `"enabled":false`) {
		t.Errorf("status = %s", st)
	}
}

func TestPlannerCategorizeThroughTheDaemon(t *testing.T) {
	d, _ := newDaemon(t)
	db := d.cfg.DB
	if _, err := db.Exec(`INSERT INTO apps (app_id, name, category_source) VALUES ('com.example.Unknown', 'Unknown', 'auto'),
		('com.example.Mine', 'Mine', 'user')`); err != nil {
		t.Fatal(err)
	}
	srv := fakeEndpointAnswering(t, `{"results":[{"appId":"com.example.Unknown","category":"Development","confidence":0.9},
		{"appId":"com.example.Mine","category":"Social","confidence":0.9}]}`)
	mustCall(t, d, "settings.set", `{"plannerEnabled":true,"plannerProvider":"remote","plannerEndpoint":"`+srv+`","plannerModel":"m"}`)

	out := mustCall(t, d, "planner.categorize", `{"appIds":["com.example.Unknown","com.example.Mine"]}`)
	if !strings.Contains(out, `"appId":"com.example.Unknown"`) || strings.Contains(out, "Mine") {
		t.Errorf("categorize = %s", out)
	}
	var cat, source string
	db.QueryRow(`SELECT categories.name, apps.category_source FROM apps JOIN categories ON categories.id = apps.category_id
		WHERE app_id = 'com.example.Unknown'`).Scan(&cat, &source)
	if cat != "Development" || source != "model" {
		t.Errorf("= (%q, %q)", cat, source)
	}

	// apps.setCategory by the user wins over the model and marks the row 'user'.
	mustCall(t, d, "apps.setCategory", `{"appId":"com.example.Unknown","categoryId":4}`)
	db.QueryRow("SELECT category_source FROM apps WHERE app_id = 'com.example.Unknown'").Scan(&source)
	if source != "user" {
		t.Errorf("source = %q after the user's choice", source)
	}
	if out := mustCall(t, d, "planner.categorize", `{}`); out != "[]" {
		t.Errorf("a second run changed something: %s", out)
	}
}

func TestPlannerParamsAreValidated(t *testing.T) {
	d, _ := newDaemon(t)
	cases := []struct{ method, params, want string }{
		{"planner.status", `{}`, "Expected void"},
		{"planner.stop", `{"x":1}`, "Expected void"},
		{"planner.setKey", ``, "Required"},
		{"planner.setKey", `{}`, "key: Required"},
		{"planner.setKey", `{"key":5}`, "key: Expected string, received number"},
		{"planner.setKey", `{"key":"` + strings.Repeat("k", 513) + `"}`, "key: String must contain at most 512"},
		{"planner.setKey", `{"key":"a b"}`, "key: must not contain whitespace"},
		{"planner.setKey", `{"key":"a\u0000b"}`, "key: must not contain"},
		{"planner.setKey", `{"key":"a","extra":1}`, "Unrecognized key"},
		{"planner.suggest", ``, "Required"},
		{"planner.suggest", `{"goal":7}`, "goal: Expected string"},
		{"planner.suggest", `{"goal":null}`, "goal: Expected string, received null"},
		{"planner.suggest", `{"goal":"` + strings.Repeat("g", 501) + `"}`, "goal: String must contain at most 500"},
		{"planner.suggest", `{"goal":"x","extra":true}`, "Unrecognized key"},
		{"planner.categorize", `{"appIds":"code"}`, "appIds: Expected array"},
		{"planner.categorize", `{"appIds":[1]}`, "appIds.0: Expected string"},
		{"planner.categorize", `{"appIds":[""]}`, "appIds.0: String must contain between 1"},
		{"planner.categorize", `{"appIds":["` + strings.Repeat("a", 301) + `"]}`, "appIds.0"},
		{"planner.categorize", `{"appIds":["` + strings.Join(make([]string, 52), `","`) + `"]}`, "appIds"},
		{"planner.categorize", `{"other":1}`, "Unrecognized key"},
		{"planner.categorize", `[]`, "Expected object"},
		{"planner.nothing", ``, "method not found"},
		{"settings.set", `{"plannerEnabled":"yes"}`, "plannerEnabled: Expected boolean"},
		{"settings.set", `{"plannerProvider":"cloud"}`, "plannerProvider: Invalid enum value"},
		{"settings.set", `{"plannerProvider":null}`, "plannerProvider"},
		{"settings.set", `{"plannerEndpoint":"ftp://x"}`, "plannerEndpoint: expected an http(s) URL"},
		{"settings.set", `{"plannerEndpoint":"https://` + strings.Repeat("a", 195) + `"}`, "plannerEndpoint: String must contain at most 200"},
		{"settings.set", `{"plannerModel":"` + strings.Repeat("m", 101) + `"}`, "plannerModel: String must contain at most 100"},
		{"settings.set", `{"plannerServerPath":"` + strings.Repeat("p", 301) + `"}`, "plannerServerPath: String must contain at most 300"},
		{"settings.set", `{"plannerModelPath":"` + strings.Repeat("p", 301) + `"}`, "plannerModelPath: String must contain at most 300"},
		{"settings.set", `{"plannerIdleMinutes":0}`, "plannerIdleMinutes: Number must be greater than or equal to 1"},
		{"settings.set", `{"plannerIdleMinutes":61}`, "plannerIdleMinutes: Number must be less than or equal to 60"},
		{"settings.set", `{"plannerIdleMinutes":1.5}`, "plannerIdleMinutes: Expected integer"},
	}
	for _, c := range cases {
		_, err := call(t, d, c.method, c.params)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s %.60s: err = %v, want %q", c.method, c.params, err, c.want)
		}
	}
	if d.planner != nil || d.settings.PlannerEnabled {
		t.Error("a rejected request switched the planner on")
	}
	// Params that are fine.
	for _, c := range []struct{ method, params string }{
		{"planner.setKey", `{"key":""}`},
		{"planner.setKey", `{"key":"  sk-ok  "}`},
		{"settings.set", `{"plannerEndpoint":"","plannerIdleMinutes":60,"plannerModel":"` + strings.Repeat("é", 100) + `"}`},
	} {
		if _, err := call(t, d, c.method, c.params); err != nil {
			t.Errorf("%s %s: %v", c.method, c.params, err)
		}
	}
}

func TestAPIKeyIsStoredPrivatelyAndNeverReturned(t *testing.T) {
	const key = "sk-RPC-SECRET-key-987"
	d, dir := newDaemon(t)
	mustCall(t, d, "planner.setKey", `{"key":"`+key+`"}`)

	path := filepath.Join(dir, "data", "planner.key")
	b, err := os.ReadFile(path)
	if err != nil || string(b) != key {
		t.Fatalf("key file: %q, %v", b, err)
	}
	if runtime.GOOS != "windows" {
		fi, _ := os.Stat(path)
		di, _ := os.Stat(filepath.Dir(path))
		if fi.Mode().Perm() != 0o600 || di.Mode().Perm() != 0o700 {
			t.Errorf("modes = %v, %v", fi.Mode().Perm(), di.Mode().Perm())
		}
	}
	// Not in the database, and not in anything the RPC returns.
	var n int
	d.cfg.DB.QueryRow("SELECT COUNT(*) FROM settings WHERE value LIKE '%' || ? || '%'", key).Scan(&n)
	if n != 0 {
		t.Error("the key is in the settings table")
	}
	srv := fakeEndpoint(t)
	for _, out := range []string{
		mustCall(t, d, "planner.status", ""),
		mustCall(t, d, "settings.get", ""),
		mustCall(t, d, "settings.set", `{"plannerEnabled":true,"plannerProvider":"remote","plannerEndpoint":"`+srv+`","plannerModel":"m"}`),
		mustCall(t, d, "planner.status", ""),
		mustCall(t, d, "planner.setKey", `{"key":"`+key+`"}`),
		mustCall(t, d, "data.export", `{"format":"json"}`),
	} {
		if strings.Contains(out, key) {
			t.Errorf("the key was returned: %s", out)
		}
	}
	if st := mustCall(t, d, "planner.status", ""); !strings.Contains(st, `"keySet":true`) {
		t.Errorf("status = %s", st)
	}

	mustCall(t, d, "planner.setKey", `{"key":""}`)
	if _, err := os.Stat(path); err == nil {
		t.Error("an empty key did not clear the file")
	}

	// A full reset forgets the key too, and switches the planner off.
	mustCall(t, d, "planner.setKey", `{"key":"`+key+`"}`)
	mustCall(t, d, "data.wipe", `{"everything":true}`)
	if _, err := os.Stat(path); err == nil {
		t.Error("data.wipe everything kept the key")
	}
	if d.planner != nil {
		t.Error("data.wipe everything left the planner on")
	}
}

func TestIsAsync(t *testing.T) {
	d, _ := newDaemon(t)
	for m, want := range map[string]bool{
		"planner.suggest": true, "planner.categorize": true,
		"planner.status": false, "planner.stop": false, "planner.setKey": false, "usage.summary": false,
	} {
		if d.IsAsync(m) != want {
			t.Errorf("IsAsync(%s) = %v", m, !want)
		}
	}
}

// A planner request in flight does not hold the daemon's lock.
func TestSlowPlannerRequestDoesNotBlockTheDaemon(t *testing.T) {
	d, _ := newDaemon(t)
	release := make(chan struct{})
	srv := fakeEndpointFunc(t, func(w http.ResponseWriter, r *http.Request) { <-release })
	mustCall(t, d, "settings.set", `{"plannerEnabled":true,"plannerProvider":"remote","plannerEndpoint":"`+srv+`","plannerModel":"m"}`)
	done := make(chan error, 1)
	go func() { _, err := call(t, d, "planner.suggest", `{}`); done <- err }()
	time.Sleep(200 * time.Millisecond)

	answered := make(chan struct{})
	go func() {
		mustCall(t, d, "usage.summary", `{"from":0,"to":1,"groupBy":"app"}`)
		mustCall(t, d, "tracker.status", "")
		d.Heartbeat()
		close(answered)
	}()
	select {
	case <-answered:
	case <-time.After(3 * time.Second):
		t.Fatal("the daemon was blocked by a planner request")
	}
	// Turning the planner off while a request waits ends the request.
	mustCall(t, d, "settings.set", `{"plannerEnabled":false}`)
	select {
	case err := <-done:
		if err == nil || err.Error() != "planner is off" {
			t.Errorf("err = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("disabling did not end the pending request")
	}
	close(release)
}

// --- a fake OpenAI-compatible endpoint on loopback --------------------------

func fakeEndpointFunc(t *testing.T, h http.HandlerFunc) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", h)
	srv := &http.Server{Handler: mux}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	return "http://" + ln.Addr().String() + "/v1"
}

func fakeEndpointAnswering(t *testing.T, content string) string {
	return fakeEndpointFunc(t, func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{"message": map[string]any{"content": content}}},
		})
	})
}

func fakeEndpoint(t *testing.T) string {
	return fakeEndpointAnswering(t, `{"summary":"A plan.","suggestions":[{"kind":"note","appId":null,"category":null,"minutes":null,"reason":"Take it easy."}]}`)
}
