package argocdinstaller

import (
	_ "embed"

	"github.com/devantler-tech/ksail/v5/pkg/svc/image/parser"
)

//go:embed Dockerfile
var dockerfile string

// chartVersion returns the pinned ArgoCD chart version extracted from the embedded Dockerfile.
func chartVersion() string {
	return parser.ParseImageFromDockerfile(
		dockerfile,
		`FROM\s+ghcr\.io/argoproj/argo-helm/argo-cd:([^\s]+)`,
		"argocd chart",
	)
}
