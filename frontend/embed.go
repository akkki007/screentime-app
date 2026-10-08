// Package frontend embeds the built Svelte UI (bun run --cwd frontend build).
package frontend

import "embed"

// Dist is the contents of dist/. A fresh checkout holds only a placeholder
// until the UI is built.
//
//go:embed all:dist
var Dist embed.FS
