// Package sandbox lets the optional planner (ADR 8) reach a model server
// that the daemon's systemd unit otherwise keeps it from.
//
// The unit runs screentimed with RestrictAddressFamilies=AF_UNIX, so the
// kernel refuses it, and every process it starts, any TCP socket. That stays
// true for everyone who leaves the planner off. Turning the planner on writes
// a drop-in that also allows AF_INET and AF_INET6 and restarts the unit once;
// turning it off removes the drop-in and restarts it back to AF_UNIX only.
package sandbox

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// DropInName is the drop-in's file name inside <unit>.d/.
const DropInName = "50-planner-network.conf"

const dropIn = `# Written by screentimed because the optional planner is on, so it can reach
# the model server you chose. Turning the planner off removes this file.
[Service]
RestrictAddressFamilies=AF_UNIX AF_INET AF_INET6
`

// Unit is the systemd user unit this daemon runs as.
type Unit struct {
	// Dir is the unit's drop-in directory, e.g.
	// ~/.config/systemd/user/screentime-daemon.service.d.
	Dir string
	// Restart reloads the manager and restarts the unit.
	Restart func() error
}

// Sync makes the drop-in match network: present when the planner is on,
// absent when it is off. It reports whether anything changed, which is when
// the unit needs a restart to apply it.
func (u *Unit) Sync(network bool) (changed bool, err error) {
	path := filepath.Join(u.Dir, DropInName)
	have, err := os.ReadFile(path)
	exists := err == nil
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	switch {
	case network && exists && bytes.Equal(have, []byte(dropIn)):
		return false, nil
	case network:
		if err := os.MkdirAll(u.Dir, 0o700); err != nil {
			return false, err
		}
		tmp := path + ".tmp"
		if err := os.WriteFile(tmp, []byte(dropIn), 0o600); err != nil {
			return false, err
		}
		return true, os.Rename(tmp, path)
	case exists:
		return true, os.Remove(path)
	}
	return false, nil
}

// DropInDir is <config>/systemd/user/<unit>.d, with config from
// XDG_CONFIG_HOME or ~/.config.
func DropInDir(unit string) (string, error) {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".config")
	}
	if unit == "" || filepath.Base(unit) != unit {
		return "", fmt.Errorf("not a unit name: %q", unit)
	}
	return filepath.Join(base, "systemd", "user", unit+".d"), nil
}
