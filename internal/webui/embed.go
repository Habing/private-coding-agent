// Package webui embeds the built Vite SPA into the Go binary so the server
// can serve the front-end without needing a separate static-file host.
//
// The dist/ directory is .gitignored except for .gitkeep; production builds
// repopulate it via the Docker multi-stage `web` step. At runtime, the SPA
// fallback gracefully no-ops when index.html is missing (only .gitkeep is
// embedded), so a Go-only build still boots — the front-end just won't serve.
package webui

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var distFS embed.FS

// FS returns the embedded dist/ subtree rooted at its top-level so callers
// can fs.ReadFile("index.html") directly.
func FS() (fs.FS, error) {
	return fs.Sub(distFS, "dist")
}
