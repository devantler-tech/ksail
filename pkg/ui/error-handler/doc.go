// Package errorhandler centralizes Cobra command execution with KSail's error formatting rules.
//
// This package provides an Executor that coordinates Cobra command execution,
// capturing stderr output and surfacing aggregated errors with proper formatting
// and normalization for user-friendly error messages.
//
// The executor intercepts Cobra's error stream, applies normalization rules
// (such as removing redundant "Error:" prefixes), and wraps the result in a
// CommandError that preserves both the formatted message and the original error
// for proper error chain semantics.
//
// Example usage:
//
//	// Create an executor with default normalizer
//	executor := errorhandler.NewExecutor()
//	err := executor.Execute(rootCmd)
//	if err != nil {
//	    // Error is a *CommandError with normalized message
//	    fmt.Fprintln(os.Stderr, err)
//	    os.Exit(1)
//	}
//
//	// Create an executor with custom normalizer
//	customNormalizer := &MyNormalizer{}
//	executor := errorhandler.NewExecutor(
//	    // default normalizer
//	)
package errorhandler
