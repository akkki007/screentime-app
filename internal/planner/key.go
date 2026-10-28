package planner

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// The remote provider's API key lives in its own file, never in the database
// (which is exported and backed up) and never in settings. The directory is
// 0700 and the file 0600. Nothing here is ever logged or returned over RPC.

// KeyPath is planner.key next to the database.
func KeyPath(dataDir string) string { return filepath.Join(dataDir, "planner.key") }

// WriteKey stores key, or removes the file when key is empty.
func WriteKey(path, key string) error {
	if path == "" {
		return errors.New("no place to store the key")
	}
	if key == "" {
		return RemoveKey(path)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return err
	}
	// Write beside the target and rename, so a crash never leaves a half key
	// and the final file is 0600 from its first byte.
	tmp, err := os.CreateTemp(dir, ".planner-key-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.WriteString(key); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// RemoveKey deletes the stored key; a missing key is not an error.
func RemoveKey(path string) error {
	if path == "" {
		return nil
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// KeySet reports whether a key is stored, without reading it.
func KeySet(path string) bool {
	if path == "" {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular() && info.Size() > 0
}

// ReadKey returns the stored key, or "" when there is none. It is only
// called when a remote request is about to be made.
func ReadKey(path string) string {
	if path == "" {
		return ""
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}
