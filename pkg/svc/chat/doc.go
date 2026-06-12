// Package chat provides services for the AI chat assistant feature.
//
// This package implements the core functionality for KSail's AI-assisted chat,
// powered by the GitHub Copilot SDK. It provides:
//
//   - Context building: Aggregates KSail documentation (pre-generated into
//     docs_generated.go by gen_docs.go), CLI help output, and KSail-specific
//     instructions into system prompt sections for the AI assistant.
//   - Tool generation: Exposes the CLI command tree as Copilot tools via
//     pkg/toolgen, which consolidates commands into permission-split tools
//     (cluster_read, cluster_write, workload_read, workload_write,
//     tenant_write, and cipher_write).
//   - Interaction handlers: Permission and elicitation handlers for the
//     non-TUI chat frontend, auto-approving reads and prompting the user
//     before write operations.
//
// Security: IsPathWithinDirectory canonicalizes paths (absolute + symlinks
// resolved) to confine file access to the current working directory and
// prevent directory traversal attacks.
package chat
