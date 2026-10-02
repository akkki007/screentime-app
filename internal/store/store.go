// Package store opens the SQLite database and applies the migrations shared
// with the Bun daemon (packages/db/migrations), tracked with PRAGMA
// user_version, so either daemon can open the other's database.
package store

import (
	"context"
	"database/sql"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/akkki007/screentime-app/packages/db/migrations"
	_ "modernc.org/sqlite" // registers the "sqlite" driver
)

// DefaultPath is $XDG_DATA_HOME/screentime/screentime.db, falling back to
// ~/.local/share like the Bun daemon (packages/shared/src/config.ts).
func DefaultPath() string {
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		base = filepath.Join(os.Getenv("HOME"), ".local", "share")
	}
	return filepath.Join(base, "screentime", "screentime.db")
}

// Open opens (creating if needed) the database at path in WAL mode, with
// foreign keys on, and applies pending migrations.
//
// The data is private (security requirement S1, issue #4): the directory is
// forced to 0700 and the database file to 0600 whether or not they already
// exist. SQLite creates the -wal and -shm files with the database file's
// mode, so they are 0600 too.
func Open(path string) (*sql.DB, error) {
	if err := privateDir(filepath.Dir(path)); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	f.Close()
	if err := os.Chmod(path, 0o600); err != nil {
		return nil, err
	}

	q := url.Values{}
	q.Add("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", "busy_timeout(5000)")
	db, err := sql.Open("sqlite", "file:"+path+"?"+q.Encode())
	if err != nil {
		return nil, err
	}
	// One connection, like bun:sqlite: writes are serialised and
	// connection-level pragmas apply to every query.
	db.SetMaxOpenConns(1)
	if err := Migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func privateDir(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	// MkdirAll leaves an existing directory's mode alone.
	return os.Chmod(dir, 0o700)
}

// Migration is one embedded .sql file.
type Migration struct {
	Version int
	Name    string
	SQL     string
}

// Migrations returns the embedded migrations in version order. Each file's
// numeric prefix must be its version, counting up from 1.
func Migrations() ([]Migration, error) {
	names, err := fs.Glob(migrations.FS, "*.sql")
	if err != nil {
		return nil, err
	}
	sort.Strings(names)
	out := make([]Migration, 0, len(names))
	for i, name := range names {
		prefix, _, _ := strings.Cut(name, "_")
		version, err := strconv.Atoi(prefix)
		if err != nil || version != i+1 {
			return nil, fmt.Errorf("migration %s: expected version %d", name, i+1)
		}
		body, err := fs.ReadFile(migrations.FS, name)
		if err != nil {
			return nil, err
		}
		out = append(out, Migration{Version: version, Name: name, SQL: string(body)})
	}
	return out, nil
}

// Migrate applies every migration newer than PRAGMA user_version, each in
// its own transaction together with the version bump.
func Migrate(db *sql.DB) error {
	all, err := Migrations()
	if err != nil {
		return err
	}
	var current int
	if err := db.QueryRow("PRAGMA user_version").Scan(&current); err != nil {
		return err
	}
	for _, m := range all {
		if m.Version <= current {
			continue
		}
		if err := applyMigration(db, m); err != nil {
			return fmt.Errorf("migration %s: %w", m.Name, err)
		}
	}
	return nil
}

func applyMigration(db *sql.DB, m Migration) error {
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(m.SQL); err != nil {
		return err
	}
	// PRAGMA takes no bound parameters; Version is an int we parsed.
	if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version = %d", m.Version)); err != nil {
		return err
	}
	return tx.Commit()
}
