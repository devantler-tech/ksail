# This file is the source of truth for container image versions used in Go code.
# Go code reads from it via go:embed.
#
# NOTE: Dependabot scanning is disabled for this file due to upstream bug
# https://github.com/dependabot/dependabot-core/issues/13383
# (multi-arch ghcr.io images cause 404 errors). Update manually until fixed.
#
# Image mappings:
# - ghcr.io/loft-sh/vcluster-pro → chartVersion() in images.go
# - ghcr.io/loft-sh/kubernetes → kubernetesVersion() in images.go

FROM ghcr.io/loft-sh/vcluster-pro:0.32.1
FROM ghcr.io/loft-sh/kubernetes:v1.32.3
