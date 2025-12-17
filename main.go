// Package main is the entry point for the KSail application.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"runtime/debug"

	"github.com/devantler-tech/ksail-go/cmd"
	"github.com/devantler-tech/ksail-go/pkg/ui/notify"
)

//nolint:gochecknoglobals
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
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

func runWithArgs(args []string) int {
	rootCmd := cmd.NewRootCmd(version, commit, date)
	rootCmd.SetArgs(args)

	err := cmd.Execute(rootCmd)
	if err != nil {
		// Extract the root cause error (last error in the chain)
		rootErr := getRootError(err)

		notify.WriteMessage(notify.Message{
			Type:    notify.ErrorType,
			Content: "%v",
			Args:    []any{rootErr},
			Writer:  rootCmd.ErrOrStderr(),
		})

		return 1
	}

	return 0
}

// getRootError extracts the root cause error by unwrapping the error chain.
func getRootError(err error) error {
	for {
		unwrapped := errors.Unwrap(err)
		if unwrapped == nil {
			return err
		}

		err = unwrapped
	}
}
