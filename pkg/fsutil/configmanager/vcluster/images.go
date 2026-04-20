package vcluster

import (
	_ "embed"

	"github.com/devantler-tech/ksail/v7/pkg/svc/image/parser"
)

// Embedded Dockerfile containing image references (Dependabot is configured to update these,
// but ghcr.io multi-arch images may not be tracked; see dependabot-core#13383).
//
//go:embed Dockerfile
var dockerfile string

// ChartVersion returns the vCluster Helm chart version from the embedded Dockerfile.
func ChartVersion() string {
	return parser.ParseImageFromDockerfile(
		dockerfile,
		`FROM\s+ghcr\.io/loft-sh/vcluster-pro:([^\s@]+)`,
		"vCluster chart",
	)
}

// KubernetesVersion returns the Kubernetes version tag from the embedded Dockerfile.
func KubernetesVersion() string {
	return parser.ParseImageFromDockerfile(
		dockerfile,
		`FROM\s+ghcr\.io/loft-sh/kubernetes:([^\s@]+)`,
		"vCluster Kubernetes",
	)
}
