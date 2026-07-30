# This file is the source of truth for container image versions used in Go code.
# Dependabot is configured to update this file, but ghcr.io multi-arch images
# may not be tracked (see dependabot-core#13383). Update manually if needed.
# Go code reads from it via go:embed.
#
# Image mappings:
# - ghcr.io/loft-sh/vcluster-pro → ChartVersion() in images.go
# - ghcr.io/loft-sh/kubernetes → KubernetesVersion() in images.go

FROM ghcr.io/loft-sh/vcluster-pro:0.36.0@sha256:07e7816415634cafd2f5a70919a7151d474b15d3e0d56225203414fb616e90f6
FROM ghcr.io/loft-sh/kubernetes:v1.36.3@sha256:00adad47a4441b13690428a5a596cd71ef2211b3674c1918e1c731bc49653ead
