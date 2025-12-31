// Package cli provides reusable helpers for command wiring and execution.
//
// This package is organized into subpackages for different functionality:
//
//   - cli/helpers: Common CLI utilities (Docker client lifecycle, editor resolution,
//     flag handling, kubeconfig path resolution)
//   - cli/lifecycle: Cluster lifecycle command helpers (start, stop, delete, etc.)
//   - cli/create: Cluster create command helpers and installer factories
//   - cli/parallel: Parallel task execution with controlled concurrency
//   - cli/runner: Command runner utilities for executing commands with output capture
//   - cli/ui: User interface components (asciiart, errorhandler, notify, timer)
//
// The utilities in this package follow dependency injection patterns and integrate
// with the KSail runtime container for testability and flexibility.
package cli
