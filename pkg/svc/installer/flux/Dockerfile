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

FROM ghcr.io/controlplaneio-fluxcd/charts/flux-operator:0.61.0@sha256:8c039527c4a20599bc49bf69fa944e4ae08a0d7c1d585664261d9e78e51b0bde
FROM ghcr.io/controlplaneio-fluxcd/flux-operator-manifests:v0.62.0@sha256:93b35c33cd8ef0aa9dfba3560f0cb5851118b2f4fd4dbae72e7b2c399e42fb3b
