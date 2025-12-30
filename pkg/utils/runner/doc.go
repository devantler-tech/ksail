// Package runner provides helpers for executing Cobra commands while capturing output.
//
// This package implements a command runner that executes Cobra commands with real-time
// console output while simultaneously capturing stdout and stderr for programmatic
// inspection. This is useful for:
//
//   - Testing commands while preserving normal console behavior
//   - Providing detailed error messages that include command output
//   - Building command orchestration tools that need output visibility
//
// The primary type is CobraCommandRunner, which implements the CommandRunner interface
// and can be used with any Cobra command. Output is displayed to the console exactly
// as it would be when running the command directly, while also being captured in a
// CommandResult for later use.
package runner
