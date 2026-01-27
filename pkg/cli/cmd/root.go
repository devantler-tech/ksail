package cmd

import (
	"fmt"

	"github.com/devantler-tech/ksail/v5/pkg/cli/cmd/cipher"
	cluster "github.com/devantler-tech/ksail/v5/pkg/cli/cmd/cluster"
	"github.com/devantler-tech/ksail/v5/pkg/cli/cmd/workload"
	"github.com/devantler-tech/ksail/v5/pkg/cli/helpers"
	"github.com/devantler-tech/ksail/v5/pkg/cli/ui/asciiart"
	"github.com/devantler-tech/ksail/v5/pkg/cli/ui/errorhandler"
	runtime "github.com/devantler-tech/ksail/v5/pkg/di"
	"github.com/spf13/cobra"
)

// NewRootCmd creates and returns the root command with version info and subcommands.
func NewRootCmd(version, commit, date string) *cobra.Command {
	runtimeContainer := runtime.NewRuntime()

	// Create the command using the helper (no field selectors needed for root command)
	cmd := &cobra.Command{
		Use:          "ksail",
		Short:        "CLI tool for operating Kubernetes",
		Long:         "CLI tool for operating Kubernetes",
		RunE:         handleRootRunE,
		SilenceUsage: true,
	}

	// Set version if available
	cmd.Version = fmt.Sprintf("%s (Built on %s from Git SHA %s)", version, date, commit)

	cmd.PersistentFlags().Bool(
		helpers.BenchmarkFlagName,
		false,
		"Show per-activity benchmark output",
	)

	// Add all subcommands
	cmd.AddCommand(cluster.NewClusterCmd(runtimeContainer))
	cmd.AddCommand(workload.NewWorkloadCmd(runtimeContainer))
	cmd.AddCommand(cipher.NewCipherCmd(runtimeContainer))
	cmd.AddCommand(NewChatCmd(runtimeContainer))
	cmd.AddCommand(NewMCPCmd(runtimeContainer))

	return cmd
}

// Execute runs the provided root command and handles errors.
func Execute(cmd *cobra.Command) error {
	executor := errorhandler.NewExecutor()

	err := executor.Execute(cmd)
	if err != nil {
		return fmt.Errorf("%w", err)
	}

	return nil
}

// --- internals ---

// handleRootRunE handles the root command.
func handleRootRunE(
	cmd *cobra.Command,
	_ []string,
) error {
	_, _ = fmt.Fprintln(cmd.OutOrStdout(), asciiart.Logo())
	_, _ = fmt.Fprintln(cmd.OutOrStdout())

	// The err can safely be ignored, as it can never fail at runtime.
	_ = cmd.Help()

	return nil
}
