// Package reconciler provides a common base for GitOps reconciliation clients.
//
// This package contains shared utilities and base types used by both
// Flux and ArgoCD reconcilers, reducing code duplication and ensuring
// consistent behavior across GitOps engines.
//
// The package provides:
//   - Base struct for building dynamic Kubernetes clients
//   - Common error handling patterns for reconciliation operations
//   - Shared timeout and polling configuration
package reconciler
