package nativehost

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"time"
)

// Run forwards every message from in to the daemon connection as a
// browser.activeTab notification. The method is fixed here and the daemon
// validates the payload, so a page can't call anything else through us.
//
// It returns nil when the browser closes stdin or the daemon goes away; the
// browser then respawns the host on its next report, which reconnects.
// stdout belongs to the browser's protocol, so diagnostics go through log
// (stderr) only.
func Run(in io.Reader, daemon net.Conn) error {
	gone := make(chan struct{})
	go func() {
		// The daemon never replies to a notification; a read ending means it
		// closed the socket.
		_, _ = io.Copy(io.Discard, daemon)
		close(gone)
	}()

	msgs := make(chan []byte)
	errs := make(chan error, 1)
	go func() {
		reader := NewReader(in)
		for {
			msg, err := reader.Next()
			if err != nil {
				errs <- err
				return
			}
			line := append([]byte(`{"jsonrpc":"2.0","method":"browser.activeTab","params":`), msg...)
			msgs <- append(line, "}\n"...)
		}
	}()

	for {
		select {
		case line := <-msgs:
			_ = daemon.SetWriteDeadline(time.Now().Add(5 * time.Second))
			if _, err := daemon.Write(line); err != nil {
				log.Printf("[native-host] daemon write failed: %v", err)
				return nil
			}
		case err := <-errs:
			if errors.Is(err, io.EOF) {
				return nil
			}
			return fmt.Errorf("reading from browser: %w", err)
		case <-gone:
			return nil
		}
	}
}
