// Package ui provides user interface components for KSail CLI.
//
// This package contains subpackages for terminal-based user interaction:
//
//   - asciiart: the KSail ASCII art logo for terminal display
//   - chat: the AI chat TUI built on Bubbletea
//   - confirm: deletion previews and confirmation prompts
//   - errorhandler: Cobra command execution with error formatting and normalization
//   - picker: a reusable interactive list picker
//
// Related packages for user interaction:
//
//   - pkg/notify: formatted message display with symbols, colors, and timing information
//   - pkg/timer: execution time tracking for single-stage and multi-stage operations
//
// The ui package components work together with those packages to provide a consistent,
// user-friendly command-line interface experience with colorized output, timing information,
// and proper error handling.
//
// Example usage:
//
//	// Display the ASCII logo
//	fmt.Println(asciiart.Logo())
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
