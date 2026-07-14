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

FROM ghcr.io/controlplaneio-fluxcd/charts/flux-operator:0.55.0@sha256:9e73c85b586f3649b317b215228ac35c1dd15a10dec87b8806f34bd7a22d42ae
FROM ghcr.io/controlplaneio-fluxcd/flux-operator-manifests:v0.55.0@sha256:4325d960b2939766bd80a99acb7a8452e6f59ecf2a57ee77b8b1f23f87ed5e5a
