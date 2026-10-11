package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/akkki007/screentime-app/internal/nativehost"
	"github.com/akkki007/screentime-app/internal/rpc"
)

// hostLinkName is the name the browsers' manifests point at: a symlink to
// screentimed, so the manifest's "path" needs no wrapper script and no
// arguments (browsers append their own).
const hostLinkName = "screentime-native-host"

func isNativeHost(args []string) bool {
	if filepath.Base(args[0]) == hostLinkName {
		return true
	}
	return len(args) > 1 && args[1] == "native-host"
}

// runNativeHost bridges the browser's stdin to the daemon. Browsers pass
// their own arguments (the extension origin, the manifest path); they carry
// nothing the host needs.
func runNativeHost() error {
	conn, err := rpc.Dial(rpc.SocketPath())
	if err != nil {
		return fmt.Errorf("cannot reach the screentime daemon (is it running?): %w", err)
	}
	defer conn.Close()
	return nativehost.Run(os.Stdin, conn)
}
