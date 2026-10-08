package shell

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// daemonMethods are the only methods the webview may forward: the daemon's
// UI-facing API. Notably not browser.activeTab (the extension's) or version
// (the shell's own handshake).
var daemonMethods = map[string]bool{
	"usage.summary": true, "usage.timeline": true, "usage.web": true,
	"limits.list": true, "limits.set": true, "limits.delete": true,
	"apps.list": true, "apps.setCategory": true, "categories.list": true,
	"settings.get": true, "settings.set": true,
	"tracker.pause": true, "tracker.resume": true, "tracker.status": true,
	"focus.start": true, "focus.stop": true,
	"data.export": true, "data.wipe": true,
}

// Bridge is the Go side of the webview's bridge. Its methods are bound to
// JavaScript, so everything here treats its arguments as untrusted.
type Bridge struct {
	Conn *Conn
	// ExportDir is where exports go (default ~/Downloads).
	ExportDir string
	// OpenDashboard, Quit and Reveal are provided by the app shell.
	OpenDashboardFn func()
	QuitFn          func()
	RevealFn        func(path string)
}

// Daemon forwards one allow-listed JSON-RPC call.
func (b *Bridge) Daemon(method string, params any) (json.RawMessage, error) {
	if !daemonMethods[method] {
		return nil, fmt.Errorf("unknown daemon method: %s", method)
	}
	return b.Conn.Call(method, params)
}

// ConnectionState is the daemon connection state, for the first paint.
func (b *Bridge) ConnectionState() State { return b.Conn.State() }

// OpenDashboard shows (or raises) the full window.
func (b *Bridge) OpenDashboard() {
	if b.OpenDashboardFn != nil {
		b.OpenDashboardFn()
	}
}

// Quit quits the UI. The daemon keeps tracking.
func (b *Bridge) Quit() {
	if b.QuitFn != nil {
		b.QuitFn()
	}
}

// ExportResult is the saved file.
type ExportResult struct {
	Path string `json:"path"`
}

// SaveExport asks the daemon for an export and writes it into the
// downloads directory (S5), then reveals it.
func (b *Bridge) SaveExport(format string, from, to *int64) (ExportResult, error) {
	if format != "csv" && format != "json" {
		return ExportResult{}, fmt.Errorf("unknown export format: %q", format)
	}
	params := map[string]any{"format": format}
	if from != nil {
		params["from"] = *from
	}
	if to != nil {
		params["to"] = *to
	}
	raw, err := b.Conn.Call("data.export", params)
	if err != nil {
		return ExportResult{}, err
	}
	var res struct {
		Filename string `json:"filename"`
		Content  string `json:"content"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		return ExportResult{}, err
	}
	dir := b.ExportDir
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return ExportResult{}, err
		}
		dir = filepath.Join(home, "Downloads")
	}
	path, err := SaveExport(dir, res.Filename, []byte(res.Content))
	if err != nil {
		return ExportResult{}, err
	}
	if b.RevealFn != nil {
		b.RevealFn(path)
	}
	return ExportResult{Path: path}, nil
}
