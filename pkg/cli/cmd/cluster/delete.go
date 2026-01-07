package cluster

import (
	"context"
	"errors"
	"fmt"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/cli/helpers"
	"github.com/devantler-tech/ksail/v5/pkg/cli/lifecycle"
	"github.com/devantler-tech/ksail/v5/pkg/cli/setup/mirrorregistry"
	runtime "github.com/devantler-tech/ksail/v5/pkg/di"
	ksailconfigmanager "github.com/devantler-tech/ksail/v5/pkg/io/config-manager/ksail"
	clusterprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster"
	clustererrors "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/errors"
	"github.com/devantler-tech/ksail/v5/pkg/utils/notify"
	"github.com/spf13/cobra"
)

// NewDeleteCmd creates and returns the delete command.
func NewDeleteCmd(runtimeContainer *runtime.Runtime) *cobra.Command {
	cmd := &cobra.Command{
		Use:           "delete",
		Short:         "Destroy a cluster",
		Long:          `Destroy a cluster.`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	cfgManager := ksailconfigmanager.NewCommandConfigManager(
		cmd,
		ksailconfigmanager.DefaultClusterFieldSelectors(),
	)

	// Add flag for controlling registry volume deletion
	cmd.Flags().
		Bool("delete-volumes", false, "Delete registry volumes when cleaning up registries")

	cmd.RunE = lifecycle.WrapHandler(runtimeContainer, cfgManager, handleDeleteRunE)

	return cmd
}

// handleDeleteRunE executes cluster deletion with registry cleanup.
func handleDeleteRunE(
	cmd *cobra.Command,
	cfgManager *ksailconfigmanager.ConfigManager,
	deps lifecycle.Deps,
) error {
	clusterCfg := cfgManager.Config
	deps = applyFactoryOverride(deps)

	// If no config file was found, try to detect distribution from kubeconfig context
	if !cfgManager.IsConfigFileFound() {
		detectedCfg, detectedDeps, detectErr := tryContextBasedDetection(cmd, clusterCfg, deps)
		if detectErr == nil {
			clusterCfg = detectedCfg
			deps = detectedDeps
		}
		// If detection fails, fall back to the default config-based approach
	}

	clusterName, err := lifecycle.GetClusterNameFromConfig(clusterCfg, deps.Factory)
	if err != nil {
		return fmt.Errorf("failed to get cluster name: %w", err)
	}

	deleteVolumes, flagErr := cmd.Flags().GetBool("delete-volumes")
	if flagErr != nil {
		return fmt.Errorf("failed to get delete-volumes flag: %w", flagErr)
	}

	if deps.Timer != nil {
		deps.Timer.NewStage()
	}

	err = executeClusterDeletion(cmd, cfgManager, deps, clusterCfg)
	if err != nil {
		return err
	}

	cleanupDeps := getCleanupDeps()
	mirrorregistry.CleanupAll(
		cmd,
		cfgManager,
		clusterCfg,
		deps,
		clusterName,
		deleteVolumes,
		cleanupDeps,
	)

	return nil
}

// tryContextBasedDetection attempts to detect the distribution and cluster name from the kubeconfig context.
// This is used when no ksail.yaml config file is found, allowing delete to work with non-scaffolded clusters.
func tryContextBasedDetection(
	cmd *cobra.Command,
	clusterCfg *v1alpha1.Cluster,
	deps lifecycle.Deps,
) (*v1alpha1.Cluster, lifecycle.Deps, error) {
	// Get current context from kubeconfig
	currentContext, err := lifecycle.GetCurrentKubeContext()
	if err != nil {
		return nil, deps, fmt.Errorf("failed to get current context: %w", err)
	}

	// Detect distribution and cluster name from context pattern
	distribution, clusterName, err := lifecycle.DetectDistributionFromContext(currentContext)
	if err != nil {
		return nil, deps, fmt.Errorf("failed to detect distribution: %w", err)
	}

	// Notify user that we auto-detected the distribution
	notify.WriteMessage(notify.Message{
		Type: notify.InfoType,
		Content: fmt.Sprintf(
			"auto-detected %s cluster '%s' from kubeconfig context",
			distribution,
			clusterName,
		),
		Writer: cmd.OutOrStdout(),
	})

	// Update the config with detected values
	clusterCfg.Spec.Cluster.Distribution = distribution
	clusterCfg.Spec.Cluster.Connection.Context = currentContext

	// Only create contextBasedFactory if there's no test factory override.
	// applyFactoryOverride sets a non-DefaultFactory when tests override the factory.
	// Note: Check both value and pointer types since WrapHandler creates a value type.
	_, isDefaultValue := deps.Factory.(clusterprovisioner.DefaultFactory)
	_, isDefaultPointer := deps.Factory.(*clusterprovisioner.DefaultFactory)

	if isDefaultValue || isDefaultPointer {
		// Create a minimal provisioner for the detected distribution.
		// Replace the DefaultFactory since it has nil DistributionConfig when no config file is found.
		// We need a contextBasedFactory that can work without distribution config.
		provisioner, provErr := lifecycle.CreateMinimalProvisioner(distribution, clusterName)
		if provErr != nil {
			return nil, deps, fmt.Errorf("failed to create provisioner: %w", provErr)
		}

		deps.Factory = &contextBasedFactory{
			distribution: distribution,
			clusterName:  clusterName,
			provisioner:  provisioner,
		}
	}

	return clusterCfg, deps, nil
}

// contextBasedFactory is a factory that returns a pre-created provisioner for context-based detection.
type contextBasedFactory struct {
	distribution v1alpha1.Distribution
	clusterName  string
	provisioner  clusterprovisioner.ClusterProvisioner
}

// Create returns the pre-created provisioner.
//
//nolint:ireturn // Factory pattern requires returning interface
func (f *contextBasedFactory) Create(
	_ context.Context,
	_ *v1alpha1.Cluster,
) (clusterprovisioner.ClusterProvisioner, any, error) {
	return f.provisioner, nil, nil
}

// applyFactoryOverride applies any test factory override to deps.
func applyFactoryOverride(deps lifecycle.Deps) lifecycle.Deps {
	clusterProvisionerFactoryMu.RLock()

	factoryOverride := clusterProvisionerFactoryOverride

	clusterProvisionerFactoryMu.RUnlock()

	if factoryOverride != nil {
		deps.Factory = factoryOverride
	}

	return deps
}

// disconnectTalosRegistriesWithContext disconnects registries from Talos network before deletion.
func disconnectTalosRegistriesWithContext(
	_ context.Context, // ctx is available via cmd.Context() in called functions
	cmd *cobra.Command,
	cfgManager *ksailconfigmanager.ConfigManager,
	clusterCfg *v1alpha1.Cluster,
	deps lifecycle.Deps,
	clusterName string,
) {
	if clusterCfg.Spec.Cluster.Distribution != v1alpha1.DistributionTalos {
		return
	}

	cleanupDeps := getCleanupDeps()

	//nolint:contextcheck // Functions use cmd.Context() internally
	mirrorregistry.DisconnectMirrorRegistriesWithWarning(cmd, cfgManager, clusterName, cleanupDeps)
	//nolint:contextcheck // Functions use cmd.Context() internally
	mirrorregistry.DisconnectLocalRegistryWithWarning(
		cmd,
		cfgManager,
		clusterCfg,
		deps,
		clusterName,
		cleanupDeps,
	)
}

// executeClusterDeletion runs the cluster deletion and handles "not found" gracefully.
func executeClusterDeletion(
	cmd *cobra.Command,
	cfgManager *ksailconfigmanager.ConfigManager,
	deps lifecycle.Deps,
	clusterCfg *v1alpha1.Cluster,
) error {
	config := lifecycle.Config{
		TitleEmoji:         "🗑️",
		TitleContent:       "Delete cluster...",
		ActivityContent:    "deleting cluster",
		SuccessContent:     "cluster deleted",
		ErrorMessagePrefix: "failed to delete cluster",
		Action: func(
			ctx context.Context,
			provisioner clusterprovisioner.ClusterProvisioner,
			name string,
		) error {
			// Check if cluster exists first
			exists, err := provisioner.Exists(ctx, name)
			if err != nil {
				return fmt.Errorf("check cluster existence: %w", err)
			}

			if !exists {
				return clustererrors.ErrClusterNotFound
			}

			// Disconnect registries before Talos cluster deletion to avoid network conflicts
			disconnectTalosRegistriesWithContext(ctx, cmd, cfgManager, clusterCfg, deps, name)

			return provisioner.Delete(ctx, name)
		},
	}

	err := lifecycle.RunWithConfig(cmd, deps, config, clusterCfg)
	if err != nil {
		if errors.Is(err, clustererrors.ErrClusterNotFound) {
			notify.WriteMessage(notify.Message{
				Type:    notify.ErrorType,
				Content: "cluster does not exist, nothing to delete",
				Timer:   helpers.MaybeTimer(cmd, deps.Timer),
				Writer:  cmd.OutOrStdout(),
			})

			return clustererrors.ErrClusterNotFound
		}

		return fmt.Errorf("cluster deletion failed: %w", err)
	}

	return nil
}
