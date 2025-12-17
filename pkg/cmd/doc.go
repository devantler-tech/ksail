// Package cmd provides reusable helpers for command wiring and execution.
//
// This package contains shared utilities that simplify the construction and execution
// of Cobra commands. Key functionality includes:
//
//   - Kubeconfig path resolution with home directory expansion
//   - Docker client lifecycle management with automatic cleanup
//   - Lifecycle command helpers for cluster operations (start, stop, delete, etc.)
//   - Command runner utilities for executing commands with output capture
//
// The utilities in this package follow dependency injection patterns and integrate
// with the KSail runtime container for testability and flexibility.
package cmd
