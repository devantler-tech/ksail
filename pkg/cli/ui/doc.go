// Package ui provides user interface components for KSail CLI.
//
// This package contains subpackages for terminal-based user interaction:
//
//   - asciiart: ASCII art rendering with color formatting for logos and graphics
//   - error-handler: Cobra command execution with error formatting and normalization
//   - notify: Formatted message display with symbols, colors, and timing information
//   - timer: Execution time tracking for single-stage and multi-stage operations
//
// The ui package components work together to provide a consistent, user-friendly
// command-line interface experience with colorized output, timing information,
// and proper error handling.
//
// Example usage:
//
//	// Display ASCII logo
//	asciiart.PrintKSailLogo(os.Stdout)
//
//	// Track command execution time
//	timer := timer.New()
//	timer.Start()
//	// ... perform operation ...
//	notify.WriteMessage(notify.Message{
//	    Type:    notify.SuccessType,
//	    Content: "Operation complete",
//	    Timer:   timer,
//	})
//
//	// Execute Cobra command with error handling
//	executor := errorhandler.NewExecutor()
//	err := executor.Execute(rootCmd)
package ui
