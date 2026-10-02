// Package apps keeps the `apps` table: one row per app ID with its display
// name, icon and category. Port of apps/daemon/src/apps.ts and
// desktop-entries.ts.
package apps

import (
	"database/sql"
	"errors"

	"github.com/akkki007/screentime-app/internal/categories"
)

func categoryID(db *sql.DB, name string) (*int64, error) {
	if name == "" {
		return nil, nil
	}
	var id int64
	err := db.QueryRow("SELECT id FROM categories WHERE name = ?", name).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &id, nil
}

func nullable(s string, ok bool) any {
	if !ok {
		return nil
	}
	return s
}

// Ensure returns the row ID for an app, creating it on first sight with its
// name, icon and default category. Existing rows are never touched, so a
// user's category choice survives.
func Ensure(db *sql.DB, appID string, lookup Lookup) (int64, error) {
	var id int64
	err := db.QueryRow("SELECT id FROM apps WHERE app_id = ?", appID).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	entry, found := lookup(appID)
	cat, err := categoryID(db, categories.DefaultFor(appID))
	if err != nil {
		return 0, err
	}
	res, err := db.Exec(
		"INSERT INTO apps (app_id, name, icon, category_id) VALUES (?, ?, ?, ?)",
		appID, nullable(entry.Name, found && entry.Name != ""), nullable(entry.Icon, found && entry.Icon != ""), cat,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// Backfill fills in names and categories for apps recorded before those
// rules existed. Only missing values are filled.
func Backfill(db *sql.DB, lookup Lookup) error {
	rows, err := db.Query("SELECT id, app_id, name IS NULL, category_id IS NULL FROM apps WHERE name IS NULL OR category_id IS NULL")
	if err != nil {
		return err
	}
	type row struct {
		id                  int64
		appID               string
		needName, needCateg bool
	}
	var todo []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.appID, &r.needName, &r.needCateg); err != nil {
			rows.Close()
			return err
		}
		todo = append(todo, r)
	}
	rows.Close()

	for _, r := range todo {
		var entry Entry
		var found bool
		if r.needName {
			entry, found = lookup(r.appID)
		}
		var cat *int64
		if r.needCateg {
			if cat, err = categoryID(db, categories.DefaultFor(r.appID)); err != nil {
				return err
			}
		}
		if _, err := db.Exec(
			"UPDATE apps SET name = COALESCE(name, ?), icon = COALESCE(icon, ?), category_id = COALESCE(category_id, ?) WHERE id = ?",
			nullable(entry.Name, found && entry.Name != ""), nullable(entry.Icon, found && entry.Icon != ""), cat, r.id,
		); err != nil {
			return err
		}
	}
	return nil
}
