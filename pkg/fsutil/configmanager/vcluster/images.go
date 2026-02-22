package vcluster

import (
	_ "embed"

	"github.com/devantler-tech/ksail/v5/pkg/svc/image/parser"
)

// Embedded Dockerfile containing image references (updated by Dependabot).
//
//go:embed Dockerfile
var dockerfile string

// ChartVersion returns the vCluster Helm chart version from the embedded Dockerfile.
func ChartVersion() string {
	return parser.ParseImageFromDockerfile(
		dockerfile,
		`FROM\s+ghcr\.io/loft-sh/vcluster-pro:([^\s]+)`,
		"vCluster chart",
	)
}

// kubernetesVersion returns the Kubernetes version tag from the embedded Dockerfile.
func kubernetesVersion() string {
	return parser.ParseImageFromDockerfile(
		dockerfile,
		`FROM\s+ghcr\.io/loft-sh/kubernetes:([^\s]+)`,
		"vCluster Kubernetes",
	)
}
