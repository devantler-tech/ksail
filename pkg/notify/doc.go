// Package notify provides utilities for sending formatted notifications to CLI users.
//
// This package includes:
//   - [WriteMessage] for displaying formatted messages with type-specific symbols and colors
//   - [ProgressGroup] for parallel task execution with live progress indicators
//   - [StageSeparatingWriter] for automatic blank line insertion between CLI stages
//
// Message types include success (✔), error (✗), warning (⚠), info (ℹ), activity (►),
// generate (✚), and title messages with customizable emojis.
//
// The [StageSeparatingWriter] wraps an io.Writer and automatically detects stage titles
// (lines starting with emojis) to insert visual separation between workflow stages.
package notify
