// Package argocd provides Argo CD GitOps integration for KSail-Go.
//
// This package is responsible for creating and maintaining Argo CD resources
// required for local GitOps workflows (e.g., repository Secret and Application).
//
// Implementations must remain credential-free: KSail-Go must not fetch or print
// Argo CD admin credentials.
package argocd
