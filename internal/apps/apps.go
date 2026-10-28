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

// defaultCategory applies the automatic layers: the substring rules, then the
// .desktop Categories. "" means neither knows the app.
func defaultCategory(appID string, e Entry) string {
	if c := categories.DefaultFor(appID); c != "" {
		return c
	}
	return categories.FromDesktop(e.Categories)
}

func nullable(s string, ok bool) any {
	if !ok {
		return nil
	}
	return s
}

// Ensure returns the row ID for an app, creating it on first sight with its
// name, icon and default category (substring rules, then .desktop
// Categories). Existing rows are never touched, so a user's category choice
// survives.
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
	cat, err := categoryID(db, defaultCategory(appID, entry))
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
// rules existed. Only missing values are filled, and only for rows whose
// category the user hasn't chosen: an auto row that is uncategorised or Other
// is re-evaluated, so a newly installed .desktop file or new rule improves it.
func Backfill(db *sql.DB, lookup Lookup) error {
	if err := resolveLegacySources(db); err != nil {
		return err
	}
	rows, err := db.Query(`SELECT apps.id, apps.app_id, apps.name IS NULL,
		apps.category_id IS NULL OR categories.name = 'Other'
		FROM apps LEFT JOIN categories ON categories.id = apps.category_id
		WHERE apps.name IS NULL OR apps.category_id IS NULL
		   OR (apps.category_source = 'auto' AND categories.name = 'Other')`)
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
		entry, found := lookup(r.appID)
		if _, err := db.Exec(
			"UPDATE apps SET name = COALESCE(name, ?), icon = COALESCE(icon, ?) WHERE id = ?",
			nullable(entry.Name, found && entry.Name != ""), nullable(entry.Icon, found && entry.Icon != ""), r.id,
		); err != nil {
			return err
		}
		if !r.needCateg {
			continue
		}
		cat, err := categoryID(db, defaultCategory(r.appID, entry))
		if err != nil {
			return err
		}
		if cat == nil {
			continue
		}
		if _, err := db.Exec(
			"UPDATE apps SET category_id = ? WHERE id = ? AND category_source != 'user'", *cat, r.id,
		); err != nil {
			return err
		}
	}
	return nil
}

// resolveLegacySources settles rows migration 0005 marked 'legacy': a row
// whose category differs from what the substring rules give was chosen by the
// user. Rows with no category, or one matching the rules, are auto.
func resolveLegacySources(db *sql.DB) error {
	rows, err := db.Query(`SELECT apps.id, apps.app_id, categories.name
		FROM apps LEFT JOIN categories ON categories.id = apps.category_id
		WHERE apps.category_source = 'legacy'`)
	if err != nil {
		return err
	}
	type row struct {
		id    int64
		appID string
		cat   sql.NullString
	}
	var todo []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.appID, &r.cat); err != nil {
			rows.Close()
			return err
		}
		todo = append(todo, r)
	}
	rows.Close()

	for _, r := range todo {
		source := "auto"
		// An unmatched app was stored uncategorised, so a category on it
		// (Other included) can only have come from the user.
		if r.cat.Valid && r.cat.String != categories.DefaultFor(r.appID) {
			source = "user"
		}
		if _, err := db.Exec("UPDATE apps SET category_source = ? WHERE id = ?", source, r.id); err != nil {
			return err
		}
	}
	return nil
}
