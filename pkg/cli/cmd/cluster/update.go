package cluster

import (
	"fmt"

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
	"github.com/spf13/cobra"
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
		changeCounts := fmt.Sprintf(
			"Would apply %d in-place, %d reboot-required, %d recreate-required changes.",
			len(diff.InPlaceChanges),
			len(diff.RebootRequired),
			len(diff.RecreateRequired),
		)

		notify.WriteMessage(notify.Message{
			Type:    notify.InfoType,
			Content: changeCounts,
			Writer:  cmd.OutOrStdout(),
		})

		notify.WriteMessage(notify.Message{
			Type:    notify.InfoType,
			Content: "Dry run complete. No changes applied.",
			Writer:  cmd.OutOrStdout(),
		})

		return nil
	}

	// If there are recreate-required changes, show clear error and require confirmation
	if diff.HasRecreateRequired() {
		for _, change := range diff.RecreateRequired {
			notify.WriteMessage(notify.Message{
				Type: notify.ErrorType,
				Content: fmt.Sprintf(
					"Cannot change %s from %s to %s in-place. %s",
					change.Field, change.OldValue, change.NewValue, change.Reason,
				),
				Writer: cmd.OutOrStderr(),
			})
		}

		notify.WriteMessage(notify.Message{
			Type: notify.WarningType,
			Content: fmt.Sprintf(
				"%d changes require cluster recreation. Use 'ksail cluster delete && ksail cluster create' or --force to recreate.",
				len(diff.RecreateRequired),
			),
			Writer: cmd.OutOrStderr(),
		})

		if !force {
			return fmt.Errorf("%d changes require cluster recreation", len(diff.RecreateRequired))
		}

		return executeRecreateFlow(cmd, cfgManager, ctx, deps, clusterName, force)
	}

	// Apply in-place and reboot-required changes
	if diff.HasInPlaceChanges() || diff.HasRebootRequired() {
		updateOpts := types.UpdateOptions{
			DryRun:        false,
			RollingReboot: true,
		}

		notify.WriteMessage(notify.Message{
			Type:    notify.ActivityType,
			Emoji:   "🔄",
			Content: "applying configuration changes in-place",
			Timer:   outputTimer,
			Writer:  cmd.OutOrStdout(),
		})

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
		reconciler := newComponentReconciler(cmd, ctx.ClusterCfg)

		componentErr := reconciler.reconcileComponents(cmd.Context(), diff, result)

		// Display results
		if len(result.AppliedChanges) > 0 {
			notify.WriteMessage(notify.Message{
				Type:    notify.SuccessType,
				Content: fmt.Sprintf("applied %d changes successfully", len(result.AppliedChanges)),
				Timer:   outputTimer,
				Writer:  cmd.OutOrStdout(),
			})
		}

		if len(result.FailedChanges) > 0 {
			notify.WriteMessage(notify.Message{
				Type:    notify.WarningType,
				Content: fmt.Sprintf("%d changes failed to apply", len(result.FailedChanges)),
				Writer:  cmd.OutOrStderr(),
			})

			for _, change := range result.FailedChanges {
				notify.WriteMessage(notify.Message{
					Type:    notify.ErrorType,
					Content: fmt.Sprintf("  - %s: %s", change.Field, change.Reason),
					Writer:  cmd.OutOrStderr(),
				})
			}
		}

		if componentErr != nil {
			return fmt.Errorf("some component changes failed to apply: %w", componentErr)
		}
	} else {
		notify.WriteMessage(notify.Message{
			Type:    notify.InfoType,
			Content: "No changes detected",
			Writer:  cmd.OutOrStdout(),
		})
	}

	return nil
}

// displayChangesSummary outputs a human-readable summary of configuration changes.
func displayChangesSummary(cmd *cobra.Command, diff *types.UpdateResult) {
	totalChanges := len(diff.InPlaceChanges) + len(diff.RebootRequired) + len(diff.RecreateRequired)

	if totalChanges == 0 {
		return
	}

	notify.WriteMessage(notify.Message{
		Type:    notify.InfoType,
		Content: fmt.Sprintf("Detected %d configuration changes:", totalChanges),
		Writer:  cmd.OutOrStdout(),
	})

	for _, change := range diff.InPlaceChanges {
		notify.WriteMessage(notify.Message{
			Type:    notify.InfoType,
			Content: fmt.Sprintf("  ✓ %s (in-place)", change.Field),
			Writer:  cmd.OutOrStdout(),
		})
	}

	for _, change := range diff.RebootRequired {
		notify.WriteMessage(notify.Message{
			Type:    notify.WarningType,
			Content: fmt.Sprintf("  ⚡ %s (reboot required)", change.Field),
			Writer:  cmd.OutOrStdout(),
		})
	}

	for _, change := range diff.RecreateRequired {
		notify.WriteMessage(notify.Message{
			Type:    notify.ErrorType,
			Content: fmt.Sprintf("  ✗ %s (recreate required)", change.Field),
			Writer:  cmd.OutOrStdout(),
		})
	}
}

// executeRecreateFlow performs the delete + create flow with confirmation.
//
//nolint:funlen // Recreate flow has sequential steps that are clearer kept together
func executeRecreateFlow(
	cmd *cobra.Command,
	cfgManager *ksailconfigmanager.ConfigManager,
	ctx *localregistry.Context,
	deps lifecycle.Deps,
	clusterName string,
	force bool,
) error {
	outputTimer := helpers.MaybeTimer(cmd, deps.Timer)

	// Show warning
	notify.WriteMessage(notify.Message{
		Type:    notify.WarningType,
		Content: "Update will delete and recreate the cluster",
		Writer:  cmd.OutOrStderr(),
	})

	notify.WriteMessage(notify.Message{
		Type:    notify.WarningType,
		Content: "All workloads and data will be lost",
		Writer:  cmd.OutOrStderr(),
	})

	// Get confirmation using the shared confirm package
	if !confirm.ShouldSkipPrompt(force) {
		notify.WriteMessage(notify.Message{
			Type:    notify.InfoType,
			Content: fmt.Sprintf("To proceed with updating cluster %q, type 'yes':", clusterName),
			Writer:  cmd.OutOrStdout(),
		})

		if !confirm.PromptForConfirmation(cmd.OutOrStdout()) {
			notify.WriteMessage(notify.Message{
				Type:    notify.InfoType,
				Content: "Update cancelled",
				Writer:  cmd.OutOrStdout(),
			})

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

	existingFields := make(map[string]bool)
	for _, c := range main.InPlaceChanges {
		existingFields[c.Field] = true
	}

	for _, c := range main.RebootRequired {
		existingFields[c.Field] = true
	}

	for _, c := range main.RecreateRequired {
		existingFields[c.Field] = true
	}

	for _, c := range provisioner.InPlaceChanges {
		if !existingFields[c.Field] {
			main.InPlaceChanges = append(main.InPlaceChanges, c)
		}
	}

	for _, c := range provisioner.RebootRequired {
		if !existingFields[c.Field] {
			main.RebootRequired = append(main.RebootRequired, c)
		}
	}

	for _, c := range provisioner.RecreateRequired {
		if !existingFields[c.Field] {
			main.RecreateRequired = append(main.RecreateRequired, c)
		}
	}
}
