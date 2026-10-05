// Command screentimed is the tracker daemon: it records which app is in
// focus, enforces the wellbeing rules and serves the UI over JSON-RPC on
// $XDG_RUNTIME_DIR/screentime/daemon.sock.
//
// Environment:
//
//	SCREENTIME_FOCUS_PROVIDER  force a focus adapter by id ("none" reports nothing)
//	SCREENTIME_DEBUG=1         log focus changes and browser reports
//	SCREENTIME_FAKE_NOW        test hook: freeze the clock at this Unix ms
//	                           (used by the contract fixtures in contract/)
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/akkki007/screentime-app/internal/apps"
	"github.com/akkki007/screentime-app/internal/daemon"
	"github.com/akkki007/screentime-app/internal/focus"
	"github.com/akkki007/screentime-app/internal/notify"
	"github.com/akkki007/screentime-app/internal/rpc"
	"github.com/akkki007/screentime-app/internal/store"
)

func main() {
	log.SetFlags(0)
	if err := run(); err != nil {
		log.Printf("[daemon] fatal error: %v", err)
		os.Exit(1)
	}
}

func clock() (func() int64, error) {
	fake := os.Getenv("SCREENTIME_FAKE_NOW")
	if fake == "" {
		return func() int64 { return time.Now().UnixMilli() }, nil
	}
	ms, err := strconv.ParseInt(fake, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("SCREENTIME_FAKE_NOW must be Unix ms: %w", err)
	}
	log.Printf("[daemon] clock frozen at %d (SCREENTIME_FAKE_NOW)", ms)
	return func() int64 { return ms }, nil
}

func run() error {
	// Everything the daemon creates is private to the user (S1).
	syscall.Umask(0o077)

	now, err := clock()
	if err != nil {
		return err
	}
	sock := rpc.SocketPath()
	if rpc.IsLive(sock) {
		return fmt.Errorf("another screentime daemon is already running")
	}

	db, err := store.Open(store.DefaultPath())
	if err != nil {
		return err
	}
	defer db.Close()

	dirs := apps.ApplicationDirs()
	candidates := []focus.Provider{
		&focus.Gnome{Now: now},
		&focus.X11{Now: now, Index: func() map[string]string { return apps.WMClassIndex(dirs) }},
	}
	provider, err := focus.Select(context.Background(), os.Getenv("SCREENTIME_FOCUS_PROVIDER"), candidates)
	if err != nil {
		return err
	}
	log.Printf("[daemon] using focus provider: %s", provider.ID())

	d, err := daemon.New(daemon.Config{
		DB:       db,
		Provider: provider,
		Notifier: &notify.DBus{},
		Now:      now,
		Lookup:   apps.DirLookup(dirs),
		Debug:    os.Getenv("SCREENTIME_DEBUG") == "1",
	})
	if err != nil {
		return err
	}
	d.Start()
	server, err := rpc.Listen(sock, d)
	if err != nil {
		return err
	}
	d.SetBroadcaster(server)
	log.Print("[daemon] listening for JSON-RPC over Unix socket")

	stop := make(chan struct{})
	go d.Run(stop)

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	<-signals
	log.Print("[daemon] shutting down")
	close(stop)
	d.Stop()
	server.Close()
	return nil
}
