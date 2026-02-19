package ciliuminstaller

import (
	_ "embed"

	"github.com/devantler-tech/ksail/v5/pkg/svc/image/parser"
)

//go:embed Dockerfile
var dockerfile string

// chartVersion returns the pinned Cilium chart version extracted from the embedded Dockerfile.
// The image tag has a v prefix but the chart version does not.
func chartVersion() string {
	return parser.ParseImageFromDockerfile(
		dockerfile,
		`FROM\s+quay\.io/cilium/cilium:v([^\s]+)`,
		"cilium chart",
	)
}
