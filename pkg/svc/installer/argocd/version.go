package argocdinstaller

import (
	_ "embed"

	"github.com/devantler-tech/ksail/v7/pkg/svc/image/parser"
)

//go:embed Dockerfile
var dockerfile string

//go:embed Dockerfile.sops
var dockerfileSops string

//go:embed Dockerfile.app
var dockerfileApp string

// chartVersion returns the pinned ArgoCD chart version extracted from the embedded Dockerfile.
func chartVersion() string {
	return parser.ParseImageFromDockerfile(
		dockerfile,
		`FROM\s+ghcr\.io/argoproj/argo-helm/argo-cd:([^\s@]+)`,
		"argocd chart",
	)
}

// sopsVersion returns the pinned SOPS version extracted from the embedded Dockerfile.sops.
func sopsVersion() string {
	return parser.ParseImageFromDockerfile(
		dockerfileSops,
		`FROM\s+ghcr\.io/getsops/sops:v([^\s]+)`,
		"sops",
	)
}

// appImage returns the full ArgoCD application image reference (e.g. "quay.io/argoproj/argocd:v3.3.1").
func appImage() string {
	tag := parser.ParseImageFromDockerfile(
		dockerfileApp,
		`FROM\s+quay\.io/argoproj/argocd:([^\s]+)`,
		"argocd app",
	)

	return "quay.io/argoproj/argocd:" + tag
}
