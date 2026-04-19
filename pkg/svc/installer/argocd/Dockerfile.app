# This file is the source of truth for the ArgoCD application image used in Go code.
# Dependabot updates this file, and Go code reads from it via go:embed.
#
# Image mappings:
# - quay.io/argoproj/argocd → ArgoCD application image (used by CMP sidecar)

FROM quay.io/argoproj/argocd:v3.3.7
