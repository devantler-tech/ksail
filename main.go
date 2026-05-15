// Package main is the entry point for the KSail application.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"runtime/debug"

	"github.com/devantler-tech/ksail/v7/internal/buildmeta"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd"
	"github.com/devantler-tech/ksail/v7/pkg/notify"
)

func main() {
	exitCode := runSafely(os.Args[1:], runWithArgs, os.Stderr)

	if exitCode != 0 {
		os.Exit(exitCode)
	}
}

//nolint:nonamedreturns // Named return simplifies panic recovery logic.
func runSafely(args []string, runner func([]string) int, errWriter io.Writer) (exitCode int) {
	defer func() {
		if r := recover(); r != nil {
			panicMessage := fmt.Sprintf("panic recovered: %v\n%s", r, debug.Stack())
			notify.WriteMessage(notify.Message{
				Type:    notify.ErrorType,
				Content: panicMessage,
				Writer:  errWriter,
			})

			exitCode = 1
		}
	}()

	exitCode = runner(args)

	return exitCode
}

// exitCodeFromError extracts a KSail-specific exit code from err if it
// implements KSailExitCode() int, returning (code, true). This intentionally
// uses a KSail-specific method name to avoid matching stdlib *exec.ExitError,
// which also implements ExitCode() int but represents real subprocess failures.
// Otherwise it returns (0, false).
func exitCodeFromError(err error) (int, bool) {
	type KSailExitCoder interface {
		KSailExitCode() int
	}

	var exitCoder KSailExitCoder
	if errors.As(err, &exitCoder) {
		return exitCoder.KSailExitCode(), true
	}

	return 0, false
}

func runWithArgs(args []string) int {
	rootCmd := cmd.NewRootCmd(buildmeta.Version, buildmeta.Commit, buildmeta.Date)
	rootCmd.SetArgs(args)

	err := cmd.Execute(rootCmd)
	if err != nil {
		// Check if this is an error with a custom exit code (e.g., DriftExitError).
		// This allows commands to return non-standard exit codes without coupling the
		// entrypoint to specific command types.
		if code, ok := exitCodeFromError(err); ok {
			// Custom exit codes (e.g., 2 for drift detected) are valid results,
			// not errors to print to stderr.
			return code
		}

		// For actual errors, print and return exit code 1.
		notify.Errorf(rootCmd.ErrOrStderr(), "%v", err)

		return 1
	}

	return 0
}
