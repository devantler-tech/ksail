// Package cmd provides the command-line interface for KSail.
//
// This package contains the root command and delegates to subcommand packages:
//   - chat: AI-assisted chat sessions powered by GitHub Copilot
//   - cipher: Secret encryption and decryption with SOPS
//   - cluster: Cluster lifecycle management (create, delete, start, stop, etc.)
//   - mcp: Model Context Protocol server for AI assistants
//   - workload: Workload management (apply, push, gen, etc.)
package cmd
