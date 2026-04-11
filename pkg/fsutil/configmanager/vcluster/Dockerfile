# This file is the source of truth for container image versions used in Go code.
# Dependabot is configured to update this file, but ghcr.io multi-arch images
# may not be tracked (see dependabot-core#13383). Update manually if needed.
# Go code reads from it via go:embed.
#
# Image mappings:
# - ghcr.io/loft-sh/vcluster-pro → chartVersion() in images.go
# - ghcr.io/loft-sh/kubernetes → KubernetesVersion() in images.go

FROM ghcr.io/loft-sh/vcluster-pro:0.33.1@sha256:29379ee7d3c41a3fb0cc5513d9b20ef856c1fe80f7311104210f20f1ab4e0a38
FROM ghcr.io/loft-sh/kubernetes:v1.35.3@sha256:946d566e0f823b79a6116ab7361f87c8d3536754bb97f3da0682de2903af1944
