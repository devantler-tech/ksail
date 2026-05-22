//go:build ui

// Package ui provides access to the optional embedded web UI. Built with the `ui` tag, this file
// embeds the compiled SPA (web/ui built into ./dist) so the operator can serve the dashboard
// directly from its own HTTP server — no separate UI container or reverse proxy required.
package ui

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var distFS embed.FS

// Assets returns the embedded SPA file system and true, since this binary was built with the UI.
func Assets() (fs.FS, bool) {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		return nil, false
	}

	return sub, true
}
