// Package migrations embeds the SQL migrations. Files are applied in name
// order by internal/store and tracked with PRAGMA user_version; their numeric
// prefix is the version they bring the database to.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
