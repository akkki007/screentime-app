//go:build gtk3

// Command screentime is the desktop app: a Wails shell around the Svelte UI
// in frontend/. The Go main process owns the connection to the daemon (ADR 3),
// the tray, and the two windows (the dashboard and the quick panel); the
// webview never opens the daemon socket.
//
// Environment:
//
//	FRONTEND_DEVSERVER_URL=<url>     development builds only: serve the UI from
//	                                 this Vite dev server instead of the embedded
//	                                 build (S4: only when asked for, never because
//	                                 a port answers). Builds with -tags production,
//	                                 which the .deb uses, ignore it.
//	SCREENTIME_SOFTWARE_RENDERING=1  turn off WebKit's GPU acceleration, for
//	                                 drivers that show a blank window
//	SCREENTIME_OPEN_PANEL=1          open the quick panel at startup (a dev aid:
//	                                 a script can't click the tray)
package main

import (
	"embed"
	"encoding/json"
	"flag"
	"io/fs"
	"log"
	"os"
	"sync"
	"time"

	"github.com/akkki007/screentime-app/frontend"
	"github.com/akkki007/screentime-app/internal/ipc"
	"github.com/akkki007/screentime-app/internal/shell"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

//go:embed icons/*.png
var icons embed.FS

const appID = "io.github.akkki007.screentime"

func main() {
	log.SetFlags(0)
	hidden := flag.Bool("hidden", false, "start in the tray only (for autostart)")
	flag.Parse()
	if err := run(*hidden); err != nil {
		log.Fatalf("[screentime] %v", err)
	}
}

func icon(name string) []byte {
	b, err := icons.ReadFile("icons/" + name + ".png")
	if err != nil {
		log.Fatalf("[screentime] missing icon %s: %v", name, err)
	}
	return b
}

func run(startHidden bool) error {
	dist, err := fs.Sub(frontend.Dist, "dist")
	if err != nil {
		return err
	}
	if _, err := fs.Stat(dist, "index.html"); err != nil {
		return &buildError{}
	}

	gpu := application.WebviewGpuPolicyOnDemand
	if os.Getenv("SCREENTIME_SOFTWARE_RENDERING") == "1" {
		gpu = application.WebviewGpuPolicyNever
	}

	var (
		app       *application.App
		main      *application.WebviewWindow
		panel     *application.WebviewWindow
		trayHost  = shell.TrayHostAvailable()
		ui        = &uiState{names: map[string]string{}}
		conn      *shell.Conn
		refreshUI func()
	)

	conn = shell.NewConn(shell.Options{
		OnNotification: func(method string, params json.RawMessage) {
			if method == "event.status" {
				var st ipc.TrackerStatus
				if json.Unmarshal(params, &st) == nil {
					ui.setStatus(&st)
					refreshUI()
					go ui.refreshToday(conn, refreshUI)
				}
			}
			app.Event.Emit("daemon:event", map[string]any{"name": method, "payload": params})
		},
		OnState: func(s shell.State) {
			ui.setConnected(s.Connected)
			if s.Connected {
				go func() {
					raw, err := conn.Call("tracker.status", nil)
					var st ipc.TrackerStatus
					if err == nil && json.Unmarshal(raw, &st) == nil {
						ui.setStatus(&st)
						refreshUI()
					}
				}()
				go ui.refreshToday(conn, refreshUI)
			}
			refreshUI()
			app.Event.Emit("daemon:connection", s)
		},
	})

	bridge := &shell.Bridge{
		Conn: conn,
		OpenDashboardFn: func() {
			panel.Hide()
			showMain(main)
		},
		QuitFn:   func() { app.Quit() },
		RevealFn: shell.Reveal,
	}

	app = application.New(application.Options{
		Name:        "Screentime",
		Description: "Local-first screentime and digital wellbeing",
		Icon:        icon("tray"),
		Services:    []application.Service{application.NewService(bridge)},
		Assets:      application.AssetOptions{Handler: application.AssetFileServerFS(dist)},
		Linux:       application.LinuxOptions{ApplicationID: appID, ProgramName: appID},
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID:               appID,
			OnSecondInstanceLaunch: func(application.SecondInstanceData) { showMain(main) },
		},
		OnShutdown: func() { conn.Stop() },
	})

	linux := application.LinuxWindow{WebviewGpuPolicy: gpu}
	main = app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:      "main",
		Title:     "Screentime",
		URL:       "/",
		Width:     1120,
		Height:    780,
		MinWidth:  760,
		MinHeight: 560,
		Hidden:    startHidden && trayHost,
		Linux:     linux,
	})
	// Closing the window leaves the app in the tray; tracking never depended
	// on it. Without a tray to come back from, closing quits the UI instead.
	main.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		if trayHost {
			main.Hide()
			e.Cancel()
		}
	})

	panelLinux := linux
	panelLinux.WindowIsTranslucent = true
	panel = app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             "panel",
		Title:            "Screentime quick panel",
		URL:              "/#widget",
		Width:            372,
		Height:           624,
		Frameless:        true,
		AlwaysOnTop:      true,
		DisableResize:    true,
		Hidden:           os.Getenv("SCREENTIME_OPEN_PANEL") != "1",
		HideOnEscape:     true,
		HideOnFocusLost:  os.Getenv("SCREENTIME_OPEN_PANEL") != "1",
		BackgroundType:   application.BackgroundTypeTransparent,
		BackgroundColour: application.NewRGBA(0, 0, 0, 0),
		Linux:            panelLinux,
	})

	tray := app.SystemTray.New()
	tray.SetIcon(icon("tray"))
	tray.AttachWindow(panel).WindowOffset(8)

	lastIcon := ""
	refreshUI = func() {
		status, connected, today, appName := ui.snapshot(conn)
		tray.SetTooltip(shell.TrayTitle(status, connected))
		if name := shell.TrayIcon(status, connected); name != lastIcon {
			lastIcon = name
			tray.SetIcon(icon(name))
		}
		var resume string
		if status != nil && status.ResumeAt != nil {
			resume = time.UnixMilli(*status.ResumeAt).Format("15:04")
		}
		menu := app.NewMenu()
		for _, item := range shell.BuildTrayMenu(status, connected, shell.TrayInfo{
			AppName: appName, TodayLabel: today, ResumeLabel: resume,
		}) {
			if item.Separator {
				menu.AddSeparator()
				continue
			}
			mi := menu.Add(item.Label).SetEnabled(!item.Disabled)
			if action, ok := shell.ParseAction(item.Action); ok {
				mi.OnClick(func(*application.Context) { handleAction(app, conn, main, panel, tray, action) })
			}
		}
		tray.SetMenu(menu)
	}
	refreshUI()
	conn.Start()
	go func() {
		for range time.Tick(time.Minute) {
			ui.refreshToday(conn, refreshUI)
		}
	}()

	return app.Run()
}

func showMain(w *application.WebviewWindow) {
	w.Show()
	w.Restore()
	w.Focus()
}

func handleAction(app *application.App, conn *shell.Conn, main, panel *application.WebviewWindow, tray *application.SystemTray, a shell.Action) {
	var err error
	switch a.Kind {
	case "open":
		showMain(main)
	case "widget":
		tray.ToggleWindow()
	case "pause":
		_, err = conn.Call("tracker.pause", map[string]int{"minutes": a.Minutes})
	case "resume":
		_, err = conn.Call("tracker.resume", nil)
	case "focus":
		_, err = conn.Call("focus.start", map[string]int{"minutes": a.Minutes})
	case "focus-stop":
		_, err = conn.Call("focus.stop", nil)
	case "quit":
		app.Quit()
	}
	if err != nil {
		log.Printf("[tray] %s failed: %v", a.Kind, err)
	}
}

// uiState is what the tray menu shows beyond the connection itself.
type uiState struct {
	mu        sync.Mutex
	status    *ipc.TrackerStatus
	connected bool
	todayMs   *int64
	names     map[string]string
}

func (u *uiState) setStatus(s *ipc.TrackerStatus) {
	u.mu.Lock()
	u.status = s
	u.mu.Unlock()
}

func (u *uiState) setConnected(c bool) {
	u.mu.Lock()
	u.connected = c
	if !c {
		u.status = nil
	}
	u.mu.Unlock()
}

func (u *uiState) snapshot(conn *shell.Conn) (status *ipc.TrackerStatus, connected bool, today, appName string) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.todayMs != nil {
		today = shell.ShortDuration(*u.todayMs)
	}
	if u.status != nil && u.status.CurrentAppID != nil {
		appName = u.names[*u.status.CurrentAppID]
		if appName == "" {
			appName = fallbackName(*u.status.CurrentAppID)
			go u.loadNames(conn)
		}
	}
	return u.status, u.connected, today, appName
}

// refreshToday keeps today's total for the tray menu.
func (u *uiState) refreshToday(conn *shell.Conn, refresh func()) {
	midnight := time.Now().Truncate(24 * time.Hour)
	y, m, d := time.Now().Date()
	midnight = time.Date(y, m, d, 0, 0, 0, 0, time.Now().Location())
	raw, err := conn.Call("usage.summary", map[string]any{
		"from": midnight.UnixMilli(), "to": time.Now().UnixMilli() + 60_000, "groupBy": "app",
	})
	if err != nil {
		return
	}
	var rows []struct {
		Ms int64 `json:"ms"`
	}
	if json.Unmarshal(raw, &rows) != nil {
		return
	}
	var total int64
	for _, r := range rows {
		total += r.Ms
	}
	u.mu.Lock()
	u.todayMs = &total
	u.mu.Unlock()
	refresh()
}

// loadNames fetches readable app names from the daemon.
func (u *uiState) loadNames(conn *shell.Conn) {
	raw, err := conn.Call("apps.list", nil)
	if err != nil {
		return
	}
	var apps []struct {
		AppID string  `json:"appId"`
		Name  *string `json:"name"`
	}
	if json.Unmarshal(raw, &apps) != nil {
		return
	}
	u.mu.Lock()
	for _, a := range apps {
		if a.Name != nil {
			u.names[a.AppID] = *a.Name
		}
	}
	u.mu.Unlock()
}

// fallbackName turns org.mozilla.firefox into "Firefox" until the daemon's
// app list has the real name.
func fallbackName(appID string) string {
	name := appID
	for i := len(appID) - 1; i >= 0; i-- {
		if appID[i] == '.' {
			name = appID[i+1:]
			break
		}
	}
	for i := 0; i < len(name); i++ {
		if name[i] == '_' {
			name = name[:i]
			break
		}
	}
	if name == "" {
		return appID
	}
	if c := name[0]; c >= 'a' && c <= 'z' {
		name = string(c-32) + name[1:]
	}
	return name
}

type buildError struct{}

func (*buildError) Error() string {
	return "the UI is not built: run `bun run build:frontend` first (or `bun run build:app`)"
}
