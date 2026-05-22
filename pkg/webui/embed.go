// Package webui embeds the built KSail web UI (the Vite/React SPA from web/ui) so the CLI can serve
// it from `ksail cluster ui` without an external web server.
//
// The assets are produced by `make ui` (npm run build in web/ui) and staged into the dist directory
// before `go build`; release binaries embed the real assets via the GoReleaser before-hook. When the
// assets are absent (a plain `go build` without `make ui`), only a committed placeholder is embedded
// and the server reports that the UI was not built.
package webui

import (
	"embed"
	"io/fs"
)

// all: includes the dotfile placeholder so the embed pattern always matches at least one file.
//
//go:embed all:dist
var assets embed.FS

// Assets returns the embedded SPA file system rooted at the dist directory.
func Assets() fs.FS {
	sub, err := fs.Sub(assets, "dist")
	if err != nil {
		// Unreachable: "dist" is a valid, constant sub-path of the embedded FS.
		panic(err)
	}

	return sub
}
