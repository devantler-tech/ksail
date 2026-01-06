// Package cli provides reusable helpers for command wiring and execution.
//
// This package is organized into subpackages for different functionality:
//
//   - cmd: Cobra command implementations for cluster, cipher, and workload commands
//   - helpers: Common CLI utilities (Docker client lifecycle, editor resolution,
//     flag handling, kubeconfig path resolution)
//   - lifecycle: Cluster lifecycle command helpers (start, stop, delete, etc.)
//   - setup: Cluster create command helpers and installer factories
//   - ui: User interface components (asciiart, errorhandler)
//
// Related packages (located in pkg/utils):
//
//   - utils/notify: Message formatting with symbols, colors, and timing
//   - utils/runner: Command runner utilities for executing commands with output capture
//   - utils/timer: Execution time tracking for operations
//
// The utilities in this package follow dependency injection patterns and integrate
// with the KSail runtime container for testability and flexibility.
package cli
