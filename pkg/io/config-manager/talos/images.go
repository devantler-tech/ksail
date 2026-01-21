package talos

import (
	_ "embed"

	"github.com/devantler-tech/ksail/v5/pkg/io/imageparser"
)

// Embedded Dockerfile containing image references (updated by Dependabot).
//
//go:embed Dockerfile
var dockerfile string

// talosImage returns the Talos container image reference from the embedded Dockerfile.
func talosImage() string {
	return imageparser.ParseImageFromDockerfile(dockerfile, `FROM\s+(ghcr\.io/siderolabs/talos:[^\s]+)`, "Talos")
}
