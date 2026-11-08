// Package errorhandler centralizes Cobra command execution with KSail's error formatting rules.
//
// This package provides an Executor that coordinates Cobra command execution. A
// command's stderr reaches the real stderr while the command runs, with publicly
// routable addresses masked, and Cobra's own error line is silenced so a failure is
// printed exactly once, by the caller.
//
// A failure is wrapped in a CommandError that preserves the original error for
// proper error chain semantics.
//
// Example usage:
//
//	executor := errorhandler.NewExecutor()
//	err := executor.Execute(rootCmd)
//	if err != nil {
//	    // Error is a *CommandError wrapping the command's error
//	    fmt.Fprintln(os.Stderr, err)
//	    os.Exit(1)
//	}
package errorhandler
