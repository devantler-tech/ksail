// Package cli provides reusable helpers for command wiring and execution.
//
// Subpackages:
//   - annotations: Command annotation constants
//   - cmd: Cobra command implementations for cluster, cipher, chat, mcp, and workload
//   - dockerutil: Docker client lifecycle helpers
//   - editor: Editor resolution for interactive editing
//   - flags: CLI flag parsing helpers
//   - kubeconfig: Kubeconfig path resolution
//   - lifecycle: Cluster lifecycle command helpers (start, stop, delete, etc.)
//   - setup: Cluster create command helpers, installer factories, and registry setup
//   - ui: User interface components (asciiart, chat TUI, confirm, errorhandler)
package cli
