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

FROM ghcr.io/controlplaneio-fluxcd/charts/flux-operator:0.59.0@sha256:ae962f87e04301c61aeb41698146027ba4c2e4b4157d4c08fb4a4e335be9aa42
FROM ghcr.io/controlplaneio-fluxcd/flux-operator-manifests:v0.59.0@sha256:2c1b16b83c0a4d53d86b71ed2b76cce6d0191df5b9eec791417ae693695f2b00
