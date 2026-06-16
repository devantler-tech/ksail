# This file is the source of truth for container image versions used in Go code.
# Dependabot is configured to update this file, but ghcr.io multi-arch images
# may not be tracked (see dependabot-core#13383). Update manually if needed.
# Go code reads from it via go:embed.
#
# Image mappings:
# - ghcr.io/loft-sh/vcluster-pro → ChartVersion() in images.go
# - ghcr.io/loft-sh/kubernetes → KubernetesVersion() in images.go

FROM ghcr.io/loft-sh/vcluster-pro:0.35.0@sha256:5afafb246b665c6dcbdf82917dc59d848d765489af89674272efaeaadb569b03
FROM ghcr.io/loft-sh/kubernetes:v1.36.1@sha256:a3b7d4e7cb7cd4a3ab120e6f0664bdea49c78f7c79d3c9961fc981928c9f89f3
