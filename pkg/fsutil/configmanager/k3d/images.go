package k3d

import (
	_ "embed"

	"github.com/devantler-tech/ksail/v5/pkg/svc/image/parser"
)

// Embedded Dockerfile containing image references (updated by Dependabot).
//
//go:embed Dockerfile
var dockerfile string

// k3sImage returns the K3s container image reference from the embedded Dockerfile.
// This ensures Go code stays in sync with Dependabot updates automatically.
// Panics if the Dockerfile cannot be parsed - this catches embedding/format issues at init time.
func k3sImage() string {
	return parser.ParseImageFromDockerfile(dockerfile, `FROM\s+(rancher/k3s:[^\s]+)`, "K3s")
}
