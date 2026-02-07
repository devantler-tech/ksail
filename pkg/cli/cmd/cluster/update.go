package cluster

import (
	"fmt"
	"strings"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/cli/annotations"
	"github.com/devantler-tech/ksail/v5/pkg/cli/helpers"
	"github.com/devantler-tech/ksail/v5/pkg/cli/lifecycle"
	"github.com/devantler-tech/ksail/v5/pkg/cli/setup/localregistry"
	"github.com/devantler-tech/ksail/v5/pkg/cli/ui/confirm"
	runtime "github.com/devantler-tech/ksail/v5/pkg/di"
	ksailconfigmanager "github.com/devantler-tech/ksail/v5/pkg/io/config-manager/ksail"
	clusterprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster"
	clustererrors "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/errors"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/types"
	"github.com/devantler-tech/ksail/v5/pkg/utils/notify"
	"github.com/devantler-tech/ksail/v5/pkg/utils/timer"
	"github.com/spf13/cobra"
)

// errRecreateChanges is returned when some changes require cluster recreation
// but --force was not specified.
var errRecreateChanges = fmt.Errorf(
	"changes require cluster recreation: %w",
	clustererrors.ErrRecreationRequired,
)

// NewUpdateCmd creates the cluster update command.
// The update command applies configuration changes to a running cluster.
// It supports in-place updates where possible and falls back to recreation when necessary.
func NewUpdateCmd(runtimeContainer *runtime.Runtime) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update a cluster configuration",
		Long: `Update a Kubernetes cluster to match the current configuration.

This command applies changes from your ksail.yaml configuration to a running cluster.

For Talos clusters, many configuration changes can be applied in-place without
cluster recreation (e.g., network settings, kubelet config, registry mirrors).

For Kind/K3d clusters, in-place updates are more limited. Worker node scaling
is supported for K3d, but most other changes require cluster recreation.

Changes are classified into three categories:
  - In-Place: Applied without disruption
  - Reboot-Required: Applied but may require node reboots
  - Recreate-Required: Require full cluster recreation

Use --dry-run to preview changes without applying them.`,
		SilenceUsage: true,
		Annotations: map[string]string{
			annotations.AnnotationPermission: "write",
		},
	}

	cfgManager := ksailconfigmanager.NewCommandConfigManager(
		cmd,
		defaultClusterMutationFieldSelectors(),
	)

	registerMirrorRegistryFlag(cmd)
	registerNameFlag(cmd, cfgManager)

	cmd.Flags().Bool("force", false,
		"Skip confirmation prompt and proceed with cluster recreation")
	_ = cfgManager.Viper.BindPFlag("force", cmd.Flags().Lookup("force"))

	cmd.Flags().Bool("dry-run", false,
		"Preview changes without applying them")
	_ = cfgManager.Viper.BindPFlag("dry-run", cmd.Flags().Lookup("dry-run"))

	cmd.RunE = lifecycle.WrapHandler(runtimeContainer, cfgManager, handleUpdateRunE)

	return cmd
}

// handleUpdateRunE executes the cluster update logic.
// It computes a diff between current and desired configuration, then applies
// changes in-place where possible, falling back to cluster recreation when necessary.
//
//nolint:cyclop,funlen // Update logic has inherent complexity from multiple code paths
func handleUpdateRunE(
	cmd *cobra.Command,
	cfgManager *ksailconfigmanager.ConfigManager,
	deps lifecycle.Deps,
) error {
	deps.Timer.Start()

	outputTimer := helpers.MaybeTimer(cmd, deps.Timer)

	// Load and validate configuration using shared helper
	ctx, clusterName, err := loadAndValidateClusterConfig(cfgManager, deps)
	if err != nil {
		return err
	}

	// Create provisioner
	factory := newProvisionerFactory(ctx)

	provisioner, _, err := factory.Create(
		cmd.Context(),
		ctx.ClusterCfg,
	)
	if err != nil {
		return fmt.Errorf("failed to create provisioner: %w", err)
	}

	// Check if cluster exists
	exists, err := provisioner.Exists(cmd.Context(), clusterName)
	if err != nil {
		return fmt.Errorf("failed to check cluster existence: %w", err)
	}

	if !exists {
		return fmt.Errorf("%w: %q", clustererrors.ErrClusterDoesNotExist, clusterName)
	}

	// Get flags
	dryRun := cfgManager.Viper.GetBool("dry-run")
	force := cfgManager.Viper.GetBool("force")

	// Check if provisioner supports updates
	updater, supportsUpdate := provisioner.(clusterprovisioner.ClusterUpdater)
	if !supportsUpdate {
		return executeRecreateFlow(cmd, cfgManager, ctx, deps, clusterName, force)
	}

	// Use DiffEngine as the central diff source for ClusterSpec-level changes
	diffEngine := NewDiffEngine(
		ctx.ClusterCfg.Spec.Cluster.Distribution,
		ctx.ClusterCfg.Spec.Cluster.Provider,
	)

	// Get current configuration from running cluster
	currentSpec, err := updater.GetCurrentConfig()
	if err != nil {
		notify.WriteMessage(notify.Message{
			Type:    notify.WarningType,
			Content: "Could not retrieve current cluster configuration, falling back to recreate",
			Writer:  cmd.OutOrStderr(),
		})

		return executeRecreateFlow(cmd, cfgManager, ctx, deps, clusterName, force)
	}

	// Use DiffEngine as the central classifier for ClusterSpec-level changes
	diff := diffEngine.ComputeDiff(currentSpec, &ctx.ClusterCfg.Spec.Cluster)

	// Augment with provisioner-specific details (node count from running state, etc.)
	provisionerDiff, err := updater.DiffConfig(
		cmd.Context(),
		clusterName,
		currentSpec,
		&ctx.ClusterCfg.Spec.Cluster,
	)
	if err != nil {
		return fmt.Errorf("failed to compute provisioner-specific diff: %w", err)
	}

	// Merge provisioner-specific changes into the main diff
	mergeProvisionerDiff(diff, provisionerDiff)

	// Display changes summary
	displayChangesSummary(cmd, diff)

	// If dry-run, show summary and exit
	if dryRun {
		var summary strings.Builder

		fmt.Fprintf(&summary,
			"Would apply %d in-place, %d reboot-required, %d recreate-required changes.\n",
			len(diff.InPlaceChanges),
			len(diff.RebootRequired),
			len(diff.RecreateRequired),
		)

		summary.WriteString("Dry run complete. No changes applied.")

		notify.Infof(cmd.OutOrStdout(), summary.String())

		return nil
	}

	// If there are recreate-required changes, show clear error and require confirmation
	if diff.HasRecreateRequired() {
		var block strings.Builder

		fmt.Fprintf(&block, "%d changes require cluster recreation:\n", len(diff.RecreateRequired))

		for _, change := range diff.RecreateRequired {
			fmt.Fprintf(&block, "  ✗ %s: cannot change from %s to %s in-place. %s\n",
				change.Field, change.OldValue, change.NewValue, change.Reason,
			)
		}

		block.WriteString(
			"Use 'ksail cluster delete && ksail cluster create' or --force to recreate.",
		)

		notify.Warningf(cmd.OutOrStderr(), block.String())

		if !force {
			return fmt.Errorf("%d %w", len(diff.RecreateRequired), errRecreateChanges)
		}

		return executeRecreateFlow(cmd, cfgManager, ctx, deps, clusterName, force)
	}

	// No actionable changes — nothing to do
	if !diff.HasInPlaceChanges() && !diff.HasRebootRequired() {
		notify.Infof(cmd.OutOrStdout(), "No changes detected")

		return nil
	}

	// Apply in-place and reboot-required changes
	reconciler := newComponentReconciler(cmd, ctx.ClusterCfg)

	return applyInPlaceChanges(
		cmd, updater, reconciler, clusterName,
		currentSpec, ctx, diff, outputTimer,
	)
}

// applyInPlaceChanges applies provisioner-level and component-level changes in-place.
func applyInPlaceChanges(
	cmd *cobra.Command,
	updater clusterprovisioner.ClusterUpdater,
	reconciler *componentReconciler,
	clusterName string,
	currentSpec *v1alpha1.ClusterSpec,
	ctx *localregistry.Context,
	diff *types.UpdateResult,
	outputTimer timer.Timer,
) error {
	updateOpts := types.UpdateOptions{
		DryRun:        false,
		RollingReboot: true,
	}

	notify.Titlef(cmd.OutOrStdout(), "🔄", "Applying changes...")

	// Apply provisioner-level changes (node scaling, Talos config, etc.)
	result, err := updater.Update(
		cmd.Context(),
		clusterName,
		currentSpec,
		&ctx.ClusterCfg.Spec.Cluster,
		updateOpts,
	)
	if err != nil {
		return fmt.Errorf("failed to apply updates: %w", err)
	}

	// Apply component-level changes (CNI, CSI, cert-manager, etc.)
	componentErr := reconciler.reconcileComponents(cmd.Context(), diff, result)

	// Display results
	if len(result.AppliedChanges) > 0 {
		notify.SuccessWithTimerf(cmd.OutOrStdout(), outputTimer,
			"applied %d changes successfully", len(result.AppliedChanges),
		)
	}

	if len(result.FailedChanges) > 0 {
		var failBlock strings.Builder

		fmt.Fprintf(&failBlock, "%d changes failed to apply:\n", len(result.FailedChanges))

		for _, change := range result.FailedChanges {
			fmt.Fprintf(&failBlock, "  - %s: %s\n", change.Field, change.Reason)
		}

		notify.Errorf(cmd.OutOrStderr(), strings.TrimRight(failBlock.String(), "\n"))
	}

	if componentErr != nil {
		return fmt.Errorf("some component changes failed to apply: %w", componentErr)
	}

	return nil
}

// displayChangesSummary outputs a human-readable summary of configuration changes
// as a single grouped block to avoid per-line symbol prefixes.
func displayChangesSummary(cmd *cobra.Command, diff *types.UpdateResult) {
	totalChanges := len(diff.InPlaceChanges) + len(diff.RebootRequired) + len(diff.RecreateRequired)

	if totalChanges == 0 {
		return
	}

	notify.Titlef(cmd.OutOrStdout(), "🔍", "Change summary")

	var block strings.Builder

	fmt.Fprintf(&block, "Detected %d configuration changes:", totalChanges)

	for _, change := range diff.InPlaceChanges {
		fmt.Fprintf(&block, "\n  ✓ %s (in-place)", change.Field)
	}

	for _, change := range diff.RebootRequired {
		fmt.Fprintf(&block, "\n  ⚡ %s (reboot required)", change.Field)
	}

	for _, change := range diff.RecreateRequired {
		fmt.Fprintf(&block, "\n  ✗ %s (recreate required)", change.Field)
	}

	notify.Infof(cmd.OutOrStdout(), block.String())
}

// executeRecreateFlow performs the delete + create flow with confirmation.
func executeRecreateFlow(
	cmd *cobra.Command,
	cfgManager *ksailconfigmanager.ConfigManager,
	ctx *localregistry.Context,
	deps lifecycle.Deps,
	clusterName string,
	force bool,
) error {
	outputTimer := helpers.MaybeTimer(cmd, deps.Timer)

	// Show warning and get confirmation
	if !confirm.ShouldSkipPrompt(force) {
		var prompt strings.Builder

		prompt.WriteString(
			"Update will delete and recreate the cluster.\n",
		)
		prompt.WriteString("All workloads and data will be lost.")

		notify.Warningf(cmd.OutOrStderr(), prompt.String())

		_, _ = fmt.Fprintf(cmd.OutOrStdout(),
			"Type \"yes\" to proceed with updating cluster %q: ", clusterName,
		)

		if !confirm.PromptForConfirmation(cmd.OutOrStdout()) {
			notify.Infof(cmd.OutOrStdout(), "Update cancelled")

			return nil
		}
	}

	// Create provisioner for delete
	factory := newProvisionerFactory(ctx)

	provisioner, _, err := factory.Create(cmd.Context(), ctx.ClusterCfg)
	if err != nil {
		return fmt.Errorf("failed to create provisioner: %w", err)
	}

	// Execute delete
	notify.WriteMessage(notify.Message{
		Type:    notify.ActivityType,
		Emoji:   "🗑️",
		Content: "deleting existing cluster",
		Timer:   outputTimer,
		Writer:  cmd.OutOrStdout(),
	})

	err = provisioner.Delete(cmd.Context(), clusterName)
	if err != nil {
		return fmt.Errorf("failed to delete existing cluster: %w", err)
	}

	notify.WriteMessage(notify.Message{
		Type:    notify.SuccessType,
		Content: "cluster deleted",
		Timer:   outputTimer,
		Writer:  cmd.OutOrStdout(),
	})

	// Execute create using shared workflow
	return runClusterCreationWorkflow(cmd, cfgManager, ctx, deps)
}

// mergeProvisionerDiff merges provisioner-specific diff results into the main diff.
// Provisioner diffs may contain distribution-specific changes (node counts, etc.)
// that the DiffEngine doesn't track. We avoid duplicating fields already covered
// by DiffEngine by checking field names.
func mergeProvisionerDiff(main, provisioner *types.UpdateResult) {
	if provisioner == nil {
		return
	}

	existingFields := collectExistingFields(main)

	main.InPlaceChanges = appendUniqueChanges(
		main.InPlaceChanges, provisioner.InPlaceChanges, existingFields,
	)
	main.RebootRequired = appendUniqueChanges(
		main.RebootRequired, provisioner.RebootRequired, existingFields,
	)
	main.RecreateRequired = appendUniqueChanges(
		main.RecreateRequired, provisioner.RecreateRequired, existingFields,
	)
}

// clusterFieldPrefix is the prefix used by DiffEngine for ClusterSpec-level fields.
// Provisioner diffs may omit this prefix — normalization strips it before dedup.
const clusterFieldPrefix = "cluster."

// normalizeFieldName strips the "cluster." prefix for deduplication purposes,
// so "cluster.vanilla.mirrorsDir" and "vanilla.mirrorsDir" are treated as the same field.
func normalizeFieldName(field string) string {
	return strings.TrimPrefix(field, clusterFieldPrefix)
}

// collectExistingFields builds a set of normalized field names already present in the diff.
func collectExistingFields(diff *types.UpdateResult) map[string]bool {
	fields := make(map[string]bool)

	for _, c := range diff.InPlaceChanges {
		fields[normalizeFieldName(c.Field)] = true
	}

	for _, c := range diff.RebootRequired {
		fields[normalizeFieldName(c.Field)] = true
	}

	for _, c := range diff.RecreateRequired {
		fields[normalizeFieldName(c.Field)] = true
	}

	return fields
}

// appendUniqueChanges appends changes from src to dst, skipping fields already in existing.
// Field names are normalized before comparison to avoid duplicates caused by prefix differences.
func appendUniqueChanges(dst, src []types.Change, existing map[string]bool) []types.Change {
	for _, c := range src {
		if !existing[normalizeFieldName(c.Field)] {
			dst = append(dst, c)
		}
	}

	return dst
}
