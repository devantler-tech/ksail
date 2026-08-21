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

FROM ghcr.io/controlplaneio-fluxcd/charts/flux-operator:0.58.1@sha256:b93eb18c9dfb62554e2cb750ecdd9f4ff58ca7e1c7bde260c6bf0c3452b786f8
FROM ghcr.io/controlplaneio-fluxcd/flux-operator-manifests:v0.58.1@sha256:16da32ffa89c350c2c9e039b6a0400758fe74e7a9a5c2440dd55f74828c2d7ed
