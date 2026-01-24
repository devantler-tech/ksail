// Package chat provides services for the AI chat assistant feature.
//
// This package implements the core functionality for KSail's AI-assisted chat,
// powered by GitHub Copilot SDK. It provides:
//
//   - Context building: Aggregates KSail documentation, CLI help, and project
//     configuration into a system context for the AI assistant.
//   - Custom tools: Read-only tools for cluster inspection (list, info, get),
//     file reading, and directory listing.
//   - Permission handling: Prompts users for confirmation on write operations
//     while auto-approving read-only operations.
//
// Security: All file and directory operations are restricted to paths within
// the current working directory to prevent directory traversal attacks.
package chat
