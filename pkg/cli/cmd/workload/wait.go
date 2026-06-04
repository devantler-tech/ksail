package workload

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"runtime"

	"github.com/devantler-tech/ksail/v7/pkg/client/kubectl"
	"github.com/spf13/cobra"
)

// NewWaitCmd creates the workload wait command.
func NewWaitCmd() *cobra.Command {
	return newKubectlCommand(func(client *kubectl.Client, kubeconfigPath string) *cobra.Command {
		return client.CreateWaitCommand(kubeconfigPath)
	})
}

// errHookFailed is returned when a pre-apply hook command fails.
var (
	errHookFailed      = errors.New("pre-apply hook failed")
	errStdinFDOverflow = errors.New("stdin file descriptor overflows int")
)

// runHooks executes hook commands sequentially via the platform shell.
// If any hook fails, execution stops and an error is returned (fail-fast).
// Stdout and stderr are forwarded to the terminal via cmd.
func runHooks(ctx context.Context, cmd *cobra.Command, hooks []string) error {
	shellName, shellArg := "sh", "-c"
	if runtime.GOOS == "windows" {
		shellName, shellArg = "cmd", "/C"
	}

	for _, hook := range hooks {
		if ctx.Err() != nil {
			return fmt.Errorf("context cancelled before hook execution: %w", ctx.Err())
		}

		//nolint:gosec // Hooks are user-provided build commands (like Tilt/Skaffold); shell execution is intentional.
		shellCmd := exec.CommandContext(ctx, shellName, shellArg, hook)
		shellCmd.Stdout = cmd.OutOrStdout()
		shellCmd.Stderr = cmd.ErrOrStderr()

		err := shellCmd.Run()
		if err != nil {
			return fmt.Errorf("%w: %q: %w", errHookFailed, hook, err)
		}
	}

	return nil
}
