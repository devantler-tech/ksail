package certmanagerinstaller

import (
	_ "embed"

	"github.com/devantler-tech/ksail/v5/pkg/svc/image/parser"
)

//go:embed Dockerfile
var dockerfile string

// chartVersion returns the pinned cert-manager chart version extracted from the embedded Dockerfile.
func chartVersion() string {
	return parser.ParseImageFromDockerfile(
		dockerfile,
		`FROM\s+quay\.io/jetstack/cert-manager-controller:([^\s]+)`,
		"cert-manager chart",
	)
}
