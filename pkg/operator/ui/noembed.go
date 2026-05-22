//go:build !ui

// Package ui provides access to the optional embedded web UI. Built without the `ui` tag (the
// default), no UI is embedded and the operator serves only the REST API. Production images are
// built with `-tags ui` after the SPA has been compiled into ./dist.
package ui

import "io/fs"

// Assets reports that no UI was built into this binary.
func Assets() (fs.FS, bool) {
	return nil, false
}
