package calicoinstaller

import (
	_ "embed"

	"github.com/devantler-tech/ksail/v5/pkg/svc/image/parser"
)

//go:embed Dockerfile
var dockerfile string

// chartVersion returns the pinned Calico chart version extracted from the embedded Dockerfile.
func chartVersion() string {
	return parser.ParseImageFromDockerfile(
		dockerfile,
		`FROM\s+docker\.io/calico/node:([^\s]+)`,
		"calico chart",
	)
}
