# This file is the source of truth for the Flux operator versions used in Go code.
# Dependabot updates this file, and Go code reads from it via go:embed.
#
# Image mappings:
# - ghcr.io/controlplaneio-fluxcd/charts/flux-operator → chart version (OCI chart artifact tag)
# - ghcr.io/controlplaneio-fluxcd/flux-operator-manifests → FluxInstance distribution
#   artifact (the OCI bundle the operator builds the Flux distribution from). It is
#   released as a matched PAIR with the chart above, so the two tags MUST be bumped
#   together — a mismatch (e.g. a floating manifests tag ahead of the pinned chart)
#   breaks Flux bootstrap when the manifests' bundled Flux distribution restructures a
#   CRD the operator patches (see ksail#5595).

FROM ghcr.io/controlplaneio-fluxcd/charts/flux-operator:0.62.1@sha256:1135514a01cc11e206f5ff0b568ac30d9defd675022b7a9da88d4a3474fbdf13
FROM ghcr.io/controlplaneio-fluxcd/flux-operator-manifests:v0.62.1@sha256:033a75c33b911d534e10c58cbcc60dda75027c99054fcd414339c9fd5019f7c3
