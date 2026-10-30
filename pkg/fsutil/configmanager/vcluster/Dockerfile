# This file is the source of truth for container image versions used in Go code.
# Dependabot is configured to update this file, but ghcr.io multi-arch images
# may not be tracked (see dependabot-core#13383). Update manually if needed.
# Go code reads from it via go:embed.
#
# Image mappings:
# - ghcr.io/loft-sh/vcluster-pro → ChartVersion() in images.go
# - ghcr.io/loft-sh/kubernetes → KubernetesVersion() in images.go

FROM ghcr.io/loft-sh/vcluster-pro:0.37.5@sha256:bf5c4aa859b32450f98f19086958f6442fb7984be75b7bedd902707e95adbb15
FROM ghcr.io/loft-sh/kubernetes:v1.37.2@sha256:77fe896b7130f4252cbb89a8e65da2d8a09d569ed554a1a53390d486a8439edb
