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
func kindNodeImage() string {
	return imageparser.ParseImageFromDockerfile(dockerfile, `FROM\s+(kindest/node:[^\s]+)`, "Kind node")
}
