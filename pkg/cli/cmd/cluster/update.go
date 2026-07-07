package cluster

import (
	"github.com/devantler-tech/ksail/v7/pkg/cli/annotations"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/clusterflags"
	"github.com/devantler-tech/ksail/v7/pkg/cli/flags"
	"github.com/devantler-tech/ksail/v7/pkg/cli/lifecycle"
	ksailconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/ksail"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// forceFlagName is the name of the confirmation-skip --force flag used by the
// delete command (where --force has never meant anything but skip-prompt).
const forceFlagName = "force"

// yesFlagName is the name of the confirmation-skip --yes flag on update. It skips
// KSail's interactive prompts only; it carries the ai.toolgen.confirm-flag
// annotation so the chat assistant auto-injects it after permission approval.
const yesFlagName = "yes"

// forceDrainFlagName is the name of the update --force-drain flag. It enables the
// destructive node-drain behavior (pods deleted directly, bypassing
// PodDisruptionBudgets) and partition wipes that the old --force flag implied. It
// is deliberately NOT a confirm-flag, so the chat assistant never injects it.
const forceDrainFlagName = "force-drain"

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
    migration); requires --force-drain
  - Rolling-Recreate: Nodes are replaced one at a time (e.g. a Talos × Hetzner
    server-type change); requires confirmation (or --yes to skip the prompt)
  - Recreate-Required: Require full cluster recreation

Confirmation vs. disruption are two separate flags:
  - --yes (-y) skips KSail's interactive confirmation prompts only.
  - --force-drain additionally lets node drains delete pods directly (bypassing
    PodDisruptionBudgets) and authorizes partition wipes — use it when a budget
    would otherwise block a rolling reboot/recreate (may cause workload
    disruption or data loss).

Use --dry-run to preview changes without applying them.
Use --output json to emit a machine-readable diff for CI/MCP consumption.`,
		SilenceUsage: true,
		Annotations: map[string]string{
			annotations.AnnotationPermission: permissionWrite,
		},
	}

	cfgManager := setupMutationCmdFlags(cmd)

	registerUpdateConsentFlags(cmd, cfgManager)

	cmd.Flags().Bool("dry-run", false,
		"Preview changes without applying them")
	_ = cfgManager.Viper.BindPFlag("dry-run", cmd.Flags().Lookup("dry-run"))

	cmd.Flags().String("output", outputFormatText,
		"Output format: text (default) or json (machine-readable, for CI/MCP)")

	cmd.RunE = lifecycle.WrapHandler(cfgManager, handleUpdateRunE)

	return cmd
}

// registerUpdateConsentFlags registers the split consent/disruption flags for
// update: --yes (skip prompts, confirm-flag annotated), --force-drain (the
// destructive PDB-bypassing drain + partition wipes), and the hidden deprecated
// --force alias that maps to --yes for one release.
func registerUpdateConsentFlags(
	cmd *cobra.Command,
	cfgManager *ksailconfigmanager.ConfigManager,
) {
	cmd.Flags().BoolP(yesFlagName, "y", false,
		"Skip KSail's interactive confirmation prompts (does NOT bypass "+
			"PodDisruptionBudgets — use --force-drain for that)")
	// --yes only skips KSail's own prompt, so the chat assistant may auto-inject
	// it after SDK-native permission approval.
	_ = cmd.Flags().SetAnnotation(
		yesFlagName, annotations.AnnotationConfirmFlag,
		[]string{annotations.AnnotationValueTrue},
	)

	cmd.Flags().Bool(forceDrainFlagName, false,
		"Make node drains delete pods directly, bypassing PodDisruptionBudgets, so a "+
			"rolling reboot/recreate completes even when a budget would block graceful "+
			"eviction; also authorizes partition wipes (may cause workload disruption "+
			"or data loss). This is the destructive behavior the old --force implied.")

	// --force is a deprecated combined flag retained for one release: it keeps its
	// full pre-split behavior (skip prompts AND the PDB-bypassing drain + partition
	// wipe) so existing scripts are unaffected, but is superseded by the narrower
	// --yes (skip prompts) and --force-drain (destructive drain). MarkDeprecated
	// hides it from help and prints a migration warning when used.
	cmd.Flags().Bool(forceFlagName, false,
		"Deprecated: use --yes (skip prompts) and/or --force-drain (destructive drain)")
	_ = cmd.Flags().MarkDeprecated(forceFlagName,
		"use --yes to skip prompts and/or --force-drain for the PDB-bypassing drain "+
			"(--force still does both for now, but will be removed)")
	_ = cfgManager.Viper.BindPFlag(forceFlagName, cmd.Flags().Lookup(forceFlagName))
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

	// Refuse to reconcile configuration to a cluster ksail did not provision. When the target is an
	// unmanaged cluster (a managed cloud cluster, a kubeadm cluster, a colleague's cluster) the guard
	// rejects here — before any change is computed or applied — so `cluster update` never mutates a
	// cluster ksail does not own. Read-only operations still work. (ksail#5885, epic #5654.)
	err = guardUpdateTargetManaged(cmd.Context(), ctx.ClusterCfg, clusterName)
	if err != nil {
		return err
	}

	clusterflags.ApplyClusterMutationFlags(cmd, ctx.ClusterCfg)

	err = validatePostMutationFlags(ctx)
	if err != nil {
		return err
	}

	// The deprecated --force retains its FULL pre-split behavior for one release: it
	// both skips prompts (like --yes) AND enables the PDB-bypassing drain + partition
	// wipe (like --force-drain). This keeps existing `update --force` scripts working
	// unchanged; --yes and --force-drain are the new, narrower replacements.
	forceDeprecated := cfgManager.Viper.GetBool(forceFlagName)

	consent := resolveConsent(
		forceDeprecated,
		cmd.Flags().Lookup(yesFlagName),
	)
	// --force-drain is not viper-bound, so read it straight off the flag set.
	forceDrainFlag, _ := cmd.Flags().GetBool(forceDrainFlagName)
	forceDrain := forceDrainFlag || forceDeprecated

	orchestrator := newUpdateOrchestrator(
		cmd, cfgManager, ctx, deps, clusterName, consent, forceDrain,
	)

	return orchestrator.run(outputTimer)
}

// resolveConsent reports whether the user consented to skip KSail's interactive
// confirmation prompts. Consent is granted by --yes or by the deprecated --force
// alias (resolved through viper). It deliberately does NOT govern the destructive
// PDB-bypassing drain or partition wipes — that is the separate --force-drain
// flag (read in handleUpdateRunE).
func resolveConsent(viperForce bool, yesFlag *pflag.Flag) bool {
	return viperForce || (yesFlag != nil && yesFlag.Changed && yesFlag.Value.String() == "true")
}

// diffExitCode is the exit code returned by the diff command when --exit-code is
// set and configuration drift is detected. This is a KSail-specific convention:
// 0 = no drift, 1 = error, 2 = drift detected.
// (Note: diff(1) uses 1 for differences; KSail reserves 1 for command errors and
// uses 2 for drift so CI scripts can distinguish drift from failures.)
const diffExitCode = 2
