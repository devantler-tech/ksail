// Package helpers provides common CLI utilities for command handling.
//
// This package consolidates various CLI helper functions that were previously
// scattered across multiple small packages (docker, editor, flags, kubeconfig).
// By grouping these related utilities together, we achieve better package cohesion
// and reduce fragmentation.
//
// Key functionality:
//   - Docker client lifecycle management (WithClient, WithClientInstance)
//   - Editor configuration resolution with proper precedence
//   - Flag handling utilities including timing detection
//   - Kubeconfig path resolution with home directory expansion
package helpers
