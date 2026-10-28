// Command screentime-fetch makes one https request for the optional planner
// (ADR 8) and exits. The daemon starts it only when the planner is on with a
// remote provider, so screentimed itself never links TLS and stays
// network-free. It reads an httpx.Request as JSON on stdin and writes an
// httpx.Reply on stdout; it logs nothing about the request.
package main

import (
	"os"

	"github.com/akkki007/screentime-app/internal/httpx/tlsx"
)

func main() {
	if err := tlsx.Serve(os.Stdin, os.Stdout, nil); err != nil {
		os.Exit(1)
	}
}
