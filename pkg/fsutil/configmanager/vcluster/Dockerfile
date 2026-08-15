# This file is the source of truth for container image versions used in Go code.
# Dependabot is configured to update this file, but ghcr.io multi-arch images
# may not be tracked (see dependabot-core#13383). Update manually if needed.
# Go code reads from it via go:embed.
#
# Image mappings:
# - ghcr.io/loft-sh/vcluster-pro → ChartVersion() in images.go
# - ghcr.io/loft-sh/kubernetes → KubernetesVersion() in images.go

FROM ghcr.io/loft-sh/vcluster-pro:0.36.1@sha256:e27fc3e7fe7eaa91d31b7bcd5fc65a1f235a21e7835237a74972074deae8fa0f
FROM ghcr.io/loft-sh/kubernetes:v1.36.3@sha256:00adad47a4441b13690428a5a596cd71ef2211b3674c1918e1c731bc49653ead
