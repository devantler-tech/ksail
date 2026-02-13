// Package main is the entry point for the KSail application.
package main

import (
	"fmt"
	"io"
	"os"
	"runtime/debug"

	"github.com/devantler-tech/ksail/v5/internal/buildmeta"
	"github.com/devantler-tech/ksail/v5/pkg/cli/cmd"
	"github.com/devantler-tech/ksail/v5/pkg/notify"
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
	rootCmd := cmd.NewRootCmd(buildmeta.Version, buildmeta.Commit, buildmeta.Date)
	rootCmd.SetArgs(args)

	err := cmd.Execute(rootCmd)
	if err != nil {
		notify.Errorf(rootCmd.ErrOrStderr(), "%v", err)

		return 1
	}

	return 0
}
