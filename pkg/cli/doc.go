// Package cli provides reusable helpers for command wiring and execution.
//
// This package is organized into subpackages for different functionality:
//
//   - cli/docker: Docker client lifecycle management with automatic cleanup
//   - cli/editor: Editor configuration resolution with proper precedence
//   - cli/flags: Flag handling utilities including timing detection
//   - cli/kubeconfig: Kubeconfig path resolution with home directory expansion
//   - cli/lifecycle: Cluster lifecycle command helpers (start, stop, delete, etc.)
//   - cli/create: Cluster create command helpers and installer factories
//   - cli/parallel: Parallel task execution with controlled concurrency
//   - cli/runner: Command runner utilities for executing commands with output capture
//   - cli/ui: User interface components (asciiart, errorhandler, notify, timer)
//
// The utilities in this package follow dependency injection patterns and integrate
// with the KSail runtime container for testability and flexibility.
package cli
