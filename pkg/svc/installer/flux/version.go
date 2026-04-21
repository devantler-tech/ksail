package fluxinstaller

import (
	_ "embed"

	"github.com/devantler-tech/ksail/v7/pkg/svc/image/parser"
)

//go:embed Dockerfile
var dockerfile string

//go:embed Dockerfile.distribution
var distributionDockerfile string

// chartVersion returns the pinned Flux operator chart version extracted from the embedded Dockerfile.
func chartVersion() string {
	return parser.ParseImageFromDockerfile(
		dockerfile,
		`FROM\s+ghcr\.io/controlplaneio-fluxcd/charts/flux-operator:([^\s@]+)`,
		"flux-operator chart",
	)
}

// distributionImages returns the Flux distribution controller images from the
// embedded Dockerfile.distribution. These are the images that the Flux operator
// deploys when creating a FluxInstance.
func distributionImages() []string {
	images := parser.ParseAllImagesFromDockerfile(distributionDockerfile)
	if len(images) == 0 {
		panic("fluxinstaller: no distribution images parsed from embedded Dockerfile.distribution")
	}

	return images
}
