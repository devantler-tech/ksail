package kind

import (
	_ "embed"

	"github.com/devantler-tech/ksail/v5/pkg/io/imageparser"
)

// Embedded Dockerfile containing image references (updated by Dependabot).
//
//go:embed Dockerfile
var dockerfile string

// kindNodeImage returns the Kind node container image reference from the embedded Dockerfile.
// This ensures Go code stays in sync with Dependabot updates automatically.
// Panics if the Dockerfile cannot be parsed - this catches embedding/format issues at init time.
func kindNodeImage() string {
	return imageparser.ParseImageFromDockerfile(
		dockerfile,
		`FROM\s+(kindest/node:[^\s]+)`,
		"Kind node",
	)
}
