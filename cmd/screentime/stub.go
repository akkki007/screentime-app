//go:build !gtk3

// Command screentime is the desktop app. It needs the GTK 3 build of Wails:
//
//	go build -tags gtk3 ./cmd/screentime
//
// (with libwebkit2gtk-4.1-dev and libgtk-3-dev installed). This stub only
// keeps `go build ./...` and `go vet ./...` working without them.
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "screentime: build with -tags gtk3 (see cmd/screentime/stub.go)")
	os.Exit(1)
}
