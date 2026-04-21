package cmd

import (
	"fmt"

	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/chat"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/cipher"
	cluster "github.com/devantler-tech/ksail/v7/pkg/cli/cmd/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/mcp"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/tenant"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/workload"
	"github.com/devantler-tech/ksail/v7/pkg/cli/flags"
	"github.com/devantler-tech/ksail/v7/pkg/cli/kubeconfighook"
	"github.com/devantler-tech/ksail/v7/pkg/cli/ui/asciiart"
	"github.com/devantler-tech/ksail/v7/pkg/cli/ui/errorhandler"
	"github.com/devantler-tech/ksail/v7/pkg/di"
	"github.com/spf13/cobra"
)

// NewRootCmd creates and returns the root command with version info and subcommands.
func NewRootCmd(version, commit, date string) *cobra.Command {
	runtimeContainer := di.NewRuntime()

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
		flags.BenchmarkFlagName,
		false,
		"Show per-activity benchmark output",
	)

	cmd.PersistentFlags().String(
		flags.ConfigFlagName,
		"",
		"Path to config file (default: ksail.yaml found via directory traversal)",
	)

	// Transparently refresh expired Omni kubeconfig tokens before any command.
	// Cobra does not chain PersistentPreRunE: when a child command defines its own
	// (e.g. workload via wrapWithKubeconfigResolution), the child's hook replaces
	// this one. Workload commands wire the hook separately in their own hook.
	cmd.PersistentPreRunE = func(child *cobra.Command, _ []string) error {
		kubeconfighook.MaybeRefreshOmniKubeconfig(child)

		return nil
	}

	// Add all subcommands
	cmd.AddCommand(cluster.NewClusterCmd(runtimeContainer))
	cmd.AddCommand(workload.NewWorkloadCmd(runtimeContainer))
	cmd.AddCommand(cipher.NewCipherCmd(runtimeContainer))
	cmd.AddCommand(chat.NewChatCmd(runtimeContainer))
	cmd.AddCommand(mcp.NewMCPCmd(runtimeContainer))
	cmd.AddCommand(tenant.NewTenantCmd(runtimeContainer))

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
