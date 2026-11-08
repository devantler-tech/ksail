# This file is the source of truth for container image versions used in Go code.
# Dependabot is configured to update this file, but ghcr.io multi-arch images
# may not be tracked (see dependabot-core#13383). Update manually if needed.
# Go code reads from it via go:embed.
#
# Image mappings:
# - ghcr.io/loft-sh/vcluster-pro → ChartVersion() in images.go
# - ghcr.io/loft-sh/kubernetes → KubernetesVersion() in images.go

FROM ghcr.io/loft-sh/vcluster-pro:0.37.6@sha256:05c2feb7edf17a57328e58a264a320a17bfc4ec4d1617552312ab93c331006ac
FROM ghcr.io/loft-sh/kubernetes:v1.37.2@sha256:dff90874d0ab8b63d5ccf277a0dcb990547f4f19df139e3c84169593b855b969
