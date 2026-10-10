//go:build gtk3

// Command screentime is the desktop app: a Wails shell around the Svelte UI
// in frontend/. The Go side owns the connection to the daemon (ADR 3); the
// webview never opens the daemon socket.
//
// It runs as two kinds of process (ADR 10). The tray process holds the tray
// and the status in its menu and never creates a webview. Each window, the
// dashboard and the quick panel, is its own `screentime -window <kind>`
// process that exits when the window closes, so an idle app costs about what
// the tray costs and closing a window gives back everything it loaded.
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
//	SCREENTIME_JIT=1                 keep JavaScriptCore's JIT on in the windows.
//	                                 It is off by default: it saves ~13 MB and a
//	                                 dashboard this size doesn't need it.
package main

import (
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"syscall"
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
	window := flag.String("window", "", "internal: run one window, \"main\" or \"panel\", as its own process")
	flag.Parse()
	var err error
	switch *window {
	case "":
		err = run(*hidden)
	case "main", "panel":
		err = runWindow(*window)
	default:
		err = fmt.Errorf("unknown window %q", *window)
	}
	if err != nil {
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

// run is the tray process.
func run(startHidden bool) error {
	dist, err := fs.Sub(frontend.Dist, "dist")
	if err != nil {
		return err
	}
	if _, err := fs.Stat(dist, "index.html"); err != nil {
		return &buildError{}
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}

	trayHost := shell.TrayHostAvailable()
	keepPanel := os.Getenv("SCREENTIME_OPEN_PANEL") == "1"
	var (
		app       *application.App
		ui        = &uiState{names: map[string]string{}}
		conn      *shell.Conn
		refreshUI func()
	)
	win := &shell.Windows{
		Exe:  exe,
		Args: func(kind string) []string { return []string{"-window", kind} },
		OnExit: func(kind string) {
			// Without a tray to come back from, closing the dashboard quits.
			if kind == "main" && !trayHost {
				app.Quit()
			}
		},
	}
	open := func(kind string, f func(string) error) {
		if err := f(kind); err != nil {
			log.Printf("[screentime] opening the %s window: %v", kind, err)
		}
	}
	openMain := func() { open("main", win.Open) }
	togglePanel := func() { open("panel", win.Toggle) }

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
		},
	})

	app = application.New(application.Options{
		Name:        "Screentime",
		Description: "Local-first screentime and digital wellbeing",
		Icon:        icon("tray"),
		Linux: application.LinuxOptions{
			ApplicationID: appID + ".Tray",
			ProgramName:   appID + ".Tray",
			// The tray keeps the app alive with no window open.
			DisableQuitOnLastWindowClosed: true,
		},
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID:               appID,
			OnSecondInstanceLaunch: func(application.SecondInstanceData) { openMain() },
		},
		OnShutdown: func() {
			win.CloseAll()
			conn.Stop()
		},
	})
	quitOnSignal(app)

	tray := app.SystemTray.New()
	tray.SetIcon(icon("tray"))
	tray.OnClick(togglePanel)

	lastIcon := ""
	var refreshMu sync.Mutex
	refreshUI = func() {
		// Status, connection and the minute timer all refresh the tray.
		refreshMu.Lock()
		defer refreshMu.Unlock()
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
				mi.OnClick(func(*application.Context) {
					handleAction(app, conn, action, openMain, togglePanel)
				})
			}
		}
		tray.SetMenu(menu)
	}
	refreshUI()
	conn.Start()
	app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		if !startHidden || !trayHost {
			openMain()
		}
		if keepPanel {
			togglePanel()
		}
	})
	go func() {
		for range time.Tick(time.Minute) {
			ui.refreshToday(conn, refreshUI)
		}
	}()

	return app.Run()
}

// quitOnSignal makes SIGTERM and SIGINT a normal quit, so the tray closes its
// windows and the daemon connection instead of dying mid-write.
func quitOnSignal(app *application.App) {
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-sig
		app.Quit()
	}()
}

// runWindow is one window's process: the dashboard or the quick panel. It
// exits when its window closes, and with it everything WebKit loaded.
func runWindow(kind string) error {
	dist, err := fs.Sub(frontend.Dist, "dist")
	if err != nil {
		return err
	}
	if _, err := fs.Stat(dist, "index.html"); err != nil {
		return &buildError{}
	}
	if os.Getenv("SCREENTIME_JIT") != "1" && os.Getenv("JSC_useJIT") == "" {
		// Read by the WebKit web process, which inherits our environment.
		_ = os.Setenv("JSC_useJIT", "false")
	}
	gpu := application.WebviewGpuPolicyOnDemand
	if os.Getenv("SCREENTIME_SOFTWARE_RENDERING") == "1" {
		gpu = application.WebviewGpuPolicyNever
	}
	// Started by the tray, stdin is a pipe from it: when that closes the tray
	// is gone (even if it was killed) and so is this window.
	fromTray := os.Getenv(shell.ParentPipeEnv) == "1"
	trayPID := os.Getppid()

	var app *application.App
	conn := shell.NewConn(shell.Options{
		OnNotification: func(method string, params json.RawMessage) {
			app.Event.Emit("daemon:event", map[string]any{"name": method, "payload": params})
		},
		OnState: func(s shell.State) { app.Event.Emit("daemon:connection", s) },
	})

	var mainWindow *application.WebviewWindow
	bridge := &shell.Bridge{
		Conn: conn,
		OpenDashboardFn: func() {
			if kind == "main" {
				if mainWindow != nil {
					mainWindow.Show()
					mainWindow.Restore()
					mainWindow.Focus()
				}
				return
			}
			// The tray owns the dashboard process: reaching it through its
			// single instance lock asks it to open one.
			if err := pokeTray(); err != nil {
				log.Printf("[screentime] opening the dashboard: %v", err)
			}
			app.Quit()
		},
		QuitFn: func() {
			// "Exit Screentime" quits the whole UI, not just this window.
			if fromTray {
				_ = syscall.Kill(trayPID, syscall.SIGTERM)
			}
			app.Quit()
		},
		CloseFn:  func() { app.Quit() },
		RevealFn: shell.Reveal,
	}

	id := appID + ".Dashboard"
	if kind == "panel" {
		id = appID + ".Panel"
	}
	app = application.New(application.Options{
		Name:        "Screentime",
		Description: "Local-first screentime and digital wellbeing",
		Icon:        icon("tray"),
		Services:    []application.Service{application.NewService(bridge)},
		Assets:      application.AssetOptions{Handler: application.AssetFileServerFS(dist)},
		Linux: application.LinuxOptions{
			// Windows keep the app id of the app itself, so GNOME matches
			// them to its launcher and icon.
			ApplicationID: appID,
			ProgramName:   appID,
		},
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: id,
			OnSecondInstanceLaunch: func(application.SecondInstanceData) {
				if kind == "main" && mainWindow != nil {
					mainWindow.Show()
					mainWindow.Restore()
					mainWindow.Focus()
				}
			},
		},
		OnShutdown: func() { conn.Stop() },
	})
	quitOnSignal(app)
	if fromTray {
		go func() {
			_, _ = io.Copy(io.Discard, os.Stdin)
			app.Quit()
		}()
	}

	switch kind {
	case "main":
		mainWindow = app.Window.NewWithOptions(application.WebviewWindowOptions{
			Name:      "main",
			Title:     "Screentime",
			URL:       "/",
			Width:     1120,
			Height:    780,
			MinWidth:  760,
			MinHeight: 560,
			Linux:     application.LinuxWindow{WebviewGpuPolicy: gpu},
		})
	case "panel":
		openPanel(app, gpu)
	}
	conn.Start()
	return app.Run()
}

// openPanel creates the quick panel: a small frameless window, the nearest
// thing to a popover that a tray icon allows (its own menu can't be styled).
// It closes when it loses focus. Wayland compositors ignore requested
// positions, so on GNOME it appears where the shell puts it; elsewhere it
// goes to the top right, where the tray is.
func openPanel(app *application.App, gpu application.WebviewGpuPolicy) {
	const width, height, inset = 372, 624, 8
	panel := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             "panel",
		Title:            "Screentime quick panel",
		URL:              "/#widget",
		Width:            width,
		Height:           height,
		Frameless:        true,
		AlwaysOnTop:      true,
		DisableResize:    true,
		BackgroundType:   application.BackgroundTypeTransparent,
		BackgroundColour: application.NewRGBA(0, 0, 0, 0),
		Linux:            application.LinuxWindow{WebviewGpuPolicy: gpu, WindowIsTranslucent: true},
	})
	if screen := app.Screen.GetPrimary(); screen != nil {
		area := screen.WorkArea
		if area.Width == 0 {
			area = screen.Bounds
		}
		panel.SetPosition(area.X+area.Width-width-inset, area.Y+inset)
	}
	if os.Getenv("SCREENTIME_OPEN_PANEL") == "1" {
		return // dev aid: stay open
	}
	openedAt := time.Now()
	panel.OnWindowEvent(events.Common.WindowLostFocus, func(*application.WindowEvent) {
		// Ignore the focus churn while the window is still being mapped.
		if time.Since(openedAt) > 700*time.Millisecond {
			panel.Close()
		}
	})
}

// pokeTray runs `screentime` with no arguments: the running tray takes it as
// a second launch and opens the dashboard; with no tray it starts one.
func pokeTray() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe)
	cmd.Env = withoutEnv(os.Environ(), shell.ParentPipeEnv)
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

func withoutEnv(env []string, name string) []string {
	out := env[:0:0]
	for _, kv := range env {
		if len(kv) > len(name) && kv[:len(name)] == name && kv[len(name)] == '=' {
			continue
		}
		out = append(out, kv)
	}
	return out
}

func handleAction(app *application.App, conn *shell.Conn, a shell.Action, openMain, togglePanel func()) {
	var err error
	switch a.Kind {
	case "open":
		openMain()
	case "widget":
		togglePanel()
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
