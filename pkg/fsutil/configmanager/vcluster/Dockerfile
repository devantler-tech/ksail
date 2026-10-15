# This file is the source of truth for container image versions used in Go code.
# Dependabot is configured to update this file, but ghcr.io multi-arch images
# may not be tracked (see dependabot-core#13383). Update manually if needed.
# Go code reads from it via go:embed.
#
# Image mappings:
# - ghcr.io/loft-sh/vcluster-pro → ChartVersion() in images.go
# - ghcr.io/loft-sh/kubernetes → KubernetesVersion() in images.go

FROM ghcr.io/loft-sh/vcluster-pro:0.37.3@sha256:843115a4c4570a0575c8d65c88b4527fd00bf012559081c39645e1880798f3fd
FROM ghcr.io/loft-sh/kubernetes:v1.36.5@sha256:85abcd891c93b945ac083891b63cee9ed203122408d51dbc2bd4bbe90a9a4a88
