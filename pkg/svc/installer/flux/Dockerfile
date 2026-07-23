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

FROM ghcr.io/controlplaneio-fluxcd/charts/flux-operator:0.56.0@sha256:1eb327c982827714f8849cfca5231aea7c070467b2a3550d7ac3112fab164a3e
FROM ghcr.io/controlplaneio-fluxcd/flux-operator-manifests:v0.56.0@sha256:e0e0861bb589f374d56383016e1ec309b8789aa1a210a44d9d1df298f3827fa6
