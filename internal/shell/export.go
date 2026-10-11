package shell

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SaveExport writes an export into dir (S5): the daemon names the file, but
// only its base name is used, the file is created 0600 with O_EXCL so an
// existing file (or a symlink planted at the path) is never overwritten or
// followed, and a numeric suffix is added on collision. It returns the path.
func SaveExport(dir, filename string, content []byte) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	base := filepath.Base(filepath.Clean("/" + strings.ReplaceAll(filename, "\\", "/")))
	if base == "/" || base == "." || base == "" {
		base = "screentime-export"
	}
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)

	for n := 0; n < 1000; n++ {
		name := base
		if n > 0 {
			name = fmt.Sprintf("%s (%d)%s", stem, n, ext)
		}
		path := filepath.Join(dir, name)
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		if _, err := f.Write(content); err != nil {
			f.Close()
			os.Remove(path)
			return "", err
		}
		return path, f.Close()
	}
	return "", errors.New("too many exports with the same name")
}
