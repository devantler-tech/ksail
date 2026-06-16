package cluster

import (
	"github.com/devantler-tech/ksail/v7/pkg/cli/annotations"
	"github.com/devantler-tech/ksail/v7/pkg/cli/flags"
	"github.com/devantler-tech/ksail/v7/pkg/cli/lifecycle"
	ksailconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/ksail"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// forceFlagName is the name of the confirmation-skip --force flag shared by
// the cluster lifecycle commands.
const forceFlagName = "force"

// NewUpdateCmd creates the cluster update command.
// The update command applies configuration changes to a running cluster.
// It supports in-place updates where possible and falls back to recreation when necessary.
func NewUpdateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update a cluster configuration",
		Long: `Update a Kubernetes cluster to match the current configuration.

This command applies changes from your ksail.yaml configuration to a running cluster.

For Talos clusters, many configuration changes can be applied in-place without
cluster recreation (e.g., network settings, kubelet config, registry mirrors).

For Kind/K3d clusters, in-place updates are more limited. Worker node scaling
is supported for K3d, but most other changes require cluster recreation.

Changes are classified into the following categories:
  - In-Place: Applied without disruption
  - Reboot-Required: Applied but may require node reboots
  - Wipe-Required: Requires wiping node partitions (e.g. disk encryption
    migration); requires --force
  - Rolling-Recreate: Nodes are replaced one at a time (e.g. a Talos × Hetzner
    server-type change); requires confirmation (or --force to skip the prompt)
  - Recreate-Required: Require full cluster recreation

Use --dry-run to preview changes without applying them.
Use --output json to emit a machine-readable diff for CI/MCP consumption.`,
		SilenceUsage: true,
		Annotations: map[string]string{
			annotations.AnnotationPermission: permissionWrite,
		},
	}

	cfgManager := setupMutationCmdFlags(cmd)

	cmd.Flags().Bool("force", false,
		"Skip confirmation prompts and proceed with cluster recreation. Also makes node "+
			"drains delete pods directly, bypassing PodDisruptionBudgets, so a rolling "+
			"reboot/recreate completes even when a budget would block graceful eviction "+
			"(may cause workload disruption or data loss)")
	_ = cmd.Flags().SetAnnotation(
		forceFlagName, annotations.AnnotationConfirmFlag,
		[]string{annotations.AnnotationValueTrue},
	)
	_ = cfgManager.Viper.BindPFlag("force", cmd.Flags().Lookup("force"))

	cmd.Flags().BoolP("yes", "y", false,
		"Skip confirmation prompt (alias for --force)")

	cmd.Flags().Bool("dry-run", false,
		"Preview changes without applying them")
	_ = cfgManager.Viper.BindPFlag("dry-run", cmd.Flags().Lookup("dry-run"))

	cmd.Flags().String("output", outputFormatText,
		"Output format: text (default) or json (machine-readable, for CI/MCP)")

	cmd.RunE = lifecycle.WrapHandler(cfgManager, handleUpdateRunE)

	return cmd
}

// handleUpdateRunE wires the resolved run state into an updateOrchestrator and
// delegates the full update lifecycle (version reconciliation, diff computation,
// apply, and recreation) to it.
func handleUpdateRunE(
	cmd *cobra.Command,
	cfgManager *ksailconfigmanager.ConfigManager,
	deps lifecycle.Deps,
) error {
	err := validateOutputFormat(cmd)
	if err != nil {
		return err
	}

	deps.Timer.Start()

	outputTimer := flags.MaybeTimer(cmd, deps.Timer)

	// Load and validate configuration using shared helper
	ctx, clusterName, err := loadAndValidateClusterConfig(cfgManager, deps)
	if err != nil {
		return err
	}

	applyClusterMutationFlags(cmd, ctx.ClusterCfg)

	err = validatePostMutationFlags(ctx)
	if err != nil {
		return err
	}

	force := resolveForce(cfgManager.Viper.GetBool("force"), cmd.Flags().Lookup("yes"))

	orchestrator := newUpdateOrchestrator(cmd, cfgManager, ctx, deps, clusterName, force)

	return orchestrator.run(outputTimer)
}

// resolveForce returns true if the viper-resolved force flag is set,
// or if the --yes flag was explicitly set to true on the command line.
// This consolidates the --force/--yes alias logic into one place.
func resolveForce(viperForce bool, yesFlag *pflag.Flag) bool {
	return viperForce || (yesFlag != nil && yesFlag.Changed && yesFlag.Value.String() == "true")
}

// diffExitCode is the exit code returned by the diff command when --exit-code is
// set and configuration drift is detected. This is a KSail-specific convention:
// 0 = no drift, 1 = error, 2 = drift detected.
// (Note: diff(1) uses 1 for differences; KSail reserves 1 for command errors and
// uses 2 for drift so CI scripts can distinguish drift from failures.)
const diffExitCode = 2
