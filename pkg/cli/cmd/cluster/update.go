package cluster

import (
	"context"
	"fmt"
	"strings"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/cli/annotations"
	"github.com/devantler-tech/ksail/v5/pkg/cli/helpers"
	"github.com/devantler-tech/ksail/v5/pkg/cli/lifecycle"
	"github.com/devantler-tech/ksail/v5/pkg/cli/setup/localregistry"
	runtime "github.com/devantler-tech/ksail/v5/pkg/di"
	configmanager "github.com/devantler-tech/ksail/v5/pkg/io/config-manager"
	ksailconfigmanager "github.com/devantler-tech/ksail/v5/pkg/io/config-manager/ksail"
	clusterprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster"
	"github.com/devantler-tech/ksail/v5/pkg/utils/notify"
	"github.com/spf13/cobra"
)

// NewUpdateCmd creates the cluster update command.
// The update command applies configuration changes to a running cluster.
// In this initial version, it uses a delete + create flow with user confirmation.
func NewUpdateCmd(runtimeContainer *runtime.Runtime) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update a cluster configuration",
		Long: `Update a Kubernetes cluster to match the current configuration.

This command applies changes from your ksail.yaml configuration to a running cluster.
Currently, updates are performed via a delete + create flow, which means:
  - All workloads and data will be lost (backup important data first)
  - The cluster will be recreated with the new configuration
  - Post-creation components (CNI, CSI, etc.) will be reinstalled

Future versions will support in-place updates for certain configuration changes.`,
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

	cmd.RunE = lifecycle.WrapHandler(runtimeContainer, cfgManager, handleUpdateRunE)

	return cmd
}

// handleUpdateRunE executes the cluster update logic.
// Currently implements a delete + create flow with confirmation prompt.
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

	// Check if cluster exists
	clusterProvisionerFactoryMu.RLock()
	factoryOverride := clusterProvisionerFactoryOverride
	clusterProvisionerFactoryMu.RUnlock()

	var factory clusterprovisioner.Factory
	if factoryOverride != nil {
		factory = factoryOverride
	} else {
		factory = clusterprovisioner.DefaultFactory{
			DistributionConfig: &clusterprovisioner.DistributionConfig{
				Kind:  ctx.KindConfig,
				K3d:   ctx.K3dConfig,
				Talos: ctx.TalosConfig,
			},
		}
	}

	provisioner, err := factory.Create(
		ctx.ClusterCfg.Spec.Cluster.Distribution,
		ctx.ClusterCfg.Spec.Cluster.Provider,
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
		return fmt.Errorf("cluster %q does not exist; use 'ksail cluster create' to create a new cluster", clusterName)
	}

	// Show warning about recreate flow
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
	force := cfgManager.Viper.GetBool("force")
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

	// Execute create (reuse existing create logic)
	return executeClusterCreation(cmd, cfgManager, ctx, deps)
}

// executeClusterCreation performs the cluster creation workflow.
// This extracts the core creation logic to be reused by both create and update commands.
func executeClusterCreation(
	cmd *cobra.Command,
	cfgManager *ksailconfigmanager.ConfigManager,
	ctx *localregistry.Context,
	deps lifecycle.Deps,
) error {
	outputTimer := helpers.MaybeTimer(cmd, deps.Timer)

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

	clusterProvisionerFactoryMu.RLock()
	factoryOverride := clusterProvisionerFactoryOverride
	clusterProvisionerFactoryMu.RUnlock()

	if factoryOverride != nil {
		deps.Factory = factoryOverride
	} else {
		deps.Factory = clusterprovisioner.DefaultFactory{
			DistributionConfig: &clusterprovisioner.DistributionConfig{
				Kind:  ctx.KindConfig,
				K3d:   ctx.K3dConfig,
				Talos: ctx.TalosConfig,
			},
		}
	}

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
