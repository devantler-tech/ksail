package k3d

import (
	_ "embed"

	"github.com/devantler-tech/ksail/v5/pkg/io/imageparser"
)

// Embedded Dockerfile containing image references (updated by Dependabot).
//
//go:embed Dockerfile
var dockerfile string

// k3sImage returns the K3s container image reference from the embedded Dockerfile.
func k3sImage() string {
	return imageparser.ParseImageFromDockerfile(dockerfile, `FROM\s+(rancher/k3s:[^\s]+)`, "K3s")
}
