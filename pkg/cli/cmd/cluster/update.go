package cluster

import (
	"fmt"
	"strings"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/cli/annotations"
	"github.com/devantler-tech/ksail/v5/pkg/cli/helpers"
	"github.com/devantler-tech/ksail/v5/pkg/cli/lifecycle"
	"github.com/devantler-tech/ksail/v5/pkg/cli/setup/localregistry"
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

	// Use the same field selectors as create command
	fieldSelectors := ksailconfigmanager.DefaultClusterFieldSelectors()
	fieldSelectors = append(fieldSelectors, ksailconfigmanager.DefaultProviderFieldSelector())
	fieldSelectors = append(fieldSelectors, ksailconfigmanager.DefaultCNIFieldSelector())
	fieldSelectors = append(fieldSelectors, ksailconfigmanager.DefaultMetricsServerFieldSelector())
	fieldSelectors = append(fieldSelectors, ksailconfigmanager.DefaultCertManagerFieldSelector())
	fieldSelectors = append(fieldSelectors, ksailconfigmanager.DefaultPolicyEngineFieldSelector())
	fieldSelectors = append(fieldSelectors, ksailconfigmanager.DefaultCSIFieldSelector())
	fieldSelectors = append(fieldSelectors, ksailconfigmanager.DefaultImportImagesFieldSelector())
	fieldSelectors = append(fieldSelectors, ksailconfigmanager.ControlPlanesFieldSelector())
	fieldSelectors = append(fieldSelectors, ksailconfigmanager.WorkersFieldSelector())

	cfgManager := ksailconfigmanager.NewCommandConfigManager(cmd, fieldSelectors)

	cmd.Flags().StringSlice("mirror-registry", []string{},
		"Configure mirror registries with format 'host=upstream' (e.g., docker.io=https://registry-1.docker.io)")
	_ = cfgManager.Viper.BindPFlag("mirror-registry", cmd.Flags().Lookup("mirror-registry"))

	cmd.Flags().StringP("name", "n", "",
		"Cluster name used for container names, registry names, and kubeconfig context")
	_ = cfgManager.Viper.BindPFlag("name", cmd.Flags().Lookup("name"))

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

	// Load cluster configuration
	ctx, err := loadClusterConfiguration(cfgManager, outputTimer)
	if err != nil {
		return err
	}

	// Apply cluster name override from --name flag if provided
	nameOverride := cfgManager.Viper.GetString("name")
	if nameOverride != "" {
		validationErr := v1alpha1.ValidateClusterName(nameOverride)
		if validationErr != nil {
			return fmt.Errorf("invalid --name flag: %w", validationErr)
		}

		err = applyClusterNameOverride(ctx, nameOverride)
		if err != nil {
			return err
		}
	}

	// Validate distribution x provider combination
	err = ctx.ClusterCfg.Spec.Cluster.Provider.ValidateForDistribution(
		ctx.ClusterCfg.Spec.Cluster.Distribution,
	)
	if err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	// Get cluster name for messaging
	clusterName := resolveClusterNameFromContext(ctx)

	// Create provisioner
	factory := getProvisionerFactory(ctx)

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
		// Fall back to recreate flow
		return executeRecreateFlow(cmd, cfgManager, ctx, deps, clusterName, force)
	}

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

	// Compute diff between current and desired configuration
	diff, err := updater.DiffConfig(
		cmd.Context(),
		clusterName,
		currentSpec,
		&ctx.ClusterCfg.Spec.Cluster,
	)
	if err != nil {
		return fmt.Errorf("failed to compute configuration diff: %w", err)
	}

	// Display changes summary
	displayChangesSummary(cmd, diff)

	// If dry-run, stop here
	if dryRun {
		notify.WriteMessage(notify.Message{
			Type:    notify.InfoType,
			Content: "Dry run complete. No changes applied.",
			Writer:  cmd.OutOrStdout(),
		})

		return nil
	}

	// If there are recreate-required changes, need confirmation
	if diff.HasRecreateRequired() {
		notify.WriteMessage(notify.Message{
			Type: notify.WarningType,
			Content: fmt.Sprintf(
				"%d changes require cluster recreation",
				len(diff.RecreateRequired),
			),
			Writer: cmd.OutOrStderr(),
		})

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

	// Get confirmation unless --force is set
	if !force {
		confirmed := promptForUpdateConfirmation(cmd, clusterName)
		if !confirmed {
			notify.WriteMessage(notify.Message{
				Type:    notify.InfoType,
				Content: "Update cancelled",
				Writer:  cmd.OutOrStdout(),
			})

			return nil
		}
	}

	// Create provisioner for delete
	factory := getProvisionerFactory(ctx)

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

	// Execute create
	return executeClusterCreation(cmd, cfgManager, ctx, deps)
}

// executeClusterCreation performs the cluster creation workflow.
// This extracts the core creation logic to be reused by both create and update commands.
//
//nolint:funlen // Creation workflow has sequential steps that are clearer kept together
func executeClusterCreation(
	cmd *cobra.Command,
	cfgManager *ksailconfigmanager.ConfigManager,
	ctx *localregistry.Context,
	deps lifecycle.Deps,
) error {
	_ = helpers.MaybeTimer(cmd, deps.Timer) // Timer value unused in this function

	localDeps := getLocalRegistryDeps()

	err := ensureLocalRegistriesReady(
		cmd,
		ctx,
		deps,
		cfgManager,
		localDeps,
	)
	if err != nil {
		return err
	}

	setupK3dMetricsServer(ctx.ClusterCfg, ctx.K3dConfig)
	SetupK3dCSI(ctx.ClusterCfg, ctx.K3dConfig)

	deps.Factory = getProvisionerFactory(ctx)

	err = executeClusterLifecycle(cmd, ctx.ClusterCfg, deps)
	if err != nil {
		return err
	}

	configureRegistryMirrorsInClusterWithWarning(
		cmd,
		ctx,
		deps,
		cfgManager,
	)

	err = localregistry.ExecuteStage(
		cmd,
		ctx,
		deps,
		localregistry.StageConnect,
		localDeps,
	)
	if err != nil {
		return fmt.Errorf("failed to connect local registry: %w", err)
	}

	err = localregistry.WaitForK3dLocalRegistryReady(
		cmd,
		ctx.ClusterCfg,
		ctx.K3dConfig,
		localDeps.DockerInvoker,
	)
	if err != nil {
		return fmt.Errorf("failed to wait for local registry: %w", err)
	}

	// Import cached images if configured
	importPath := ctx.ClusterCfg.Spec.Cluster.ImportImages
	if importPath != "" {
		if ctx.ClusterCfg.Spec.Cluster.Distribution == v1alpha1.DistributionTalos {
			notify.WriteMessage(notify.Message{
				Type:    notify.WarningType,
				Content: "image import is not supported for Talos clusters; ignoring --import-images value %q",
				Args:    []any{importPath},
				Writer:  cmd.OutOrStderr(),
			})
		} else {
			err = importCachedImages(cmd, ctx, importPath, deps.Timer)
			if err != nil {
				notify.WriteMessage(notify.Message{
					Type:    notify.WarningType,
					Content: "failed to import images from %s: %v",
					Args:    []any{importPath, err},
					Writer:  cmd.OutOrStderr(),
				})
			}
		}
	}

	return handlePostCreationSetup(cmd, ctx.ClusterCfg, deps.Timer)
}

// promptForUpdateConfirmation prompts the user to confirm cluster update.
// Returns true if the user confirms, false otherwise.
func promptForUpdateConfirmation(cmd *cobra.Command, clusterName string) bool {
	notify.WriteMessage(notify.Message{
		Type:    notify.InfoType,
		Content: fmt.Sprintf("To proceed with updating cluster %q, type 'yes':", clusterName),
		Writer:  cmd.OutOrStdout(),
	})

	var response string

	_, err := fmt.Fscanln(cmd.InOrStdin(), &response)
	if err != nil {
		return false
	}

	return strings.TrimSpace(strings.ToLower(response)) == "yes"
}

// getProvisionerFactory returns the cluster provisioner factory, using any override if set.
//
//nolint:ireturn // Factory interface is appropriate for dependency injection
func getProvisionerFactory(ctx *localregistry.Context) clusterprovisioner.Factory {
	clusterProvisionerFactoryMu.RLock()

	factoryOverride := clusterProvisionerFactoryOverride

	clusterProvisionerFactoryMu.RUnlock()

	if factoryOverride != nil {
		return factoryOverride
	}

	return clusterprovisioner.DefaultFactory{
		DistributionConfig: &clusterprovisioner.DistributionConfig{
			Kind:  ctx.KindConfig,
			K3d:   ctx.K3dConfig,
			Talos: ctx.TalosConfig,
		},
	}
}
