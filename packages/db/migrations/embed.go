// Package migrations embeds the SQL migrations shared by the Bun daemon
// (packages/db) and the Go daemon (internal/store). Files are applied in
// name order and tracked with PRAGMA user_version; their numeric prefix is
// the version they bring the database to.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
