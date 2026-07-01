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

// distributionArtifact returns the FluxInstance distribution OCI artifact reference,
// pinned to the flux-operator-manifests version extracted from the embedded Dockerfile.
// Pinning this (rather than floating ":latest") keeps it a matched pair with the
// chart above so an upstream manifests release cannot silently break Flux bootstrap
// (ksail#5595). The digest in the Dockerfile is for Dependabot; the FluxInstance
// artifact field takes the tag form.
func distributionArtifact() string {
	version := parser.ParseImageFromDockerfile(
		dockerfile,
		`FROM\s+ghcr\.io/controlplaneio-fluxcd/flux-operator-manifests:([^\s@]+)`,
		"flux-operator manifests",
	)

	return "oci://ghcr.io/controlplaneio-fluxcd/flux-operator-manifests:" + version
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
