package cluster

import (
	"context"
	"fmt"

	"github.com/devantler-tech/ksail-go/pkg/apis/cluster/v1alpha1"
	dockerclient "github.com/devantler-tech/ksail-go/pkg/client/docker"
	cmdhelpers "github.com/devantler-tech/ksail-go/pkg/cmd"
	runtime "github.com/devantler-tech/ksail-go/pkg/di"
	k3dconfigmanager "github.com/devantler-tech/ksail-go/pkg/io/config-manager/k3d"
	kindconfigmanager "github.com/devantler-tech/ksail-go/pkg/io/config-manager/kind"
	ksailconfigmanager "github.com/devantler-tech/ksail-go/pkg/io/config-manager/ksail"
	clusterprovisioner "github.com/devantler-tech/ksail-go/pkg/svc/provisioner/cluster"
	k3dprovisioner "github.com/devantler-tech/ksail-go/pkg/svc/provisioner/cluster/k3d"
	kindprovisioner "github.com/devantler-tech/ksail-go/pkg/svc/provisioner/cluster/kind"
	"github.com/devantler-tech/ksail-go/pkg/svc/provisioner/registry"
	"github.com/devantler-tech/ksail-go/pkg/ui/notify"
	"github.com/devantler-tech/ksail-go/pkg/ui/timer"
	"github.com/docker/docker/client"
	"github.com/spf13/cobra"
)

// newDeleteLifecycleConfig creates the lifecycle configuration for cluster deletion.
func newDeleteLifecycleConfig() cmdhelpers.LifecycleConfig {
	return cmdhelpers.LifecycleConfig{
		TitleEmoji:         "🗑️",
		TitleContent:       "Delete cluster...",
		ActivityContent:    "deleting cluster",
		SuccessContent:     "cluster deleted",
		ErrorMessagePrefix: "failed to delete cluster",
		Action: func(ctx context.Context, provisioner clusterprovisioner.ClusterProvisioner, clusterName string) error {
			return provisioner.Delete(ctx, clusterName)
		},
	}
}

// NewDeleteCmd creates and returns the delete command.
func NewDeleteCmd(runtimeContainer *runtime.Runtime) *cobra.Command {
	cmd := &cobra.Command{
		Use:          "delete",
		Short:        "Destroy a cluster",
		Long:         `Destroy a cluster.`,
		SilenceUsage: true,
	}

	cfgManager := ksailconfigmanager.NewCommandConfigManager(
		cmd,
		ksailconfigmanager.DefaultClusterFieldSelectors(),
	)

	// Add flag for controlling registry volume deletion
	cmd.Flags().
		Bool("delete-volumes", false, "Delete registry volumes when cleaning up registries")

	cmd.RunE = cmdhelpers.WrapLifecycleHandler(runtimeContainer, cfgManager, handleDeleteRunE)

	return cmd
}

// handleDeleteRunE executes cluster deletion with registry cleanup.
func handleDeleteRunE(
	cmd *cobra.Command,
	cfgManager *ksailconfigmanager.ConfigManager,
	deps cmdhelpers.LifecycleDeps,
) error {
	config := newDeleteLifecycleConfig()

	// Execute cluster deletion
	err := cmdhelpers.HandleLifecycleRunE(cmd, cfgManager, deps, config)
	if err != nil {
		return fmt.Errorf("cluster deletion failed: %w", err)
	}

	clusterCfg := cfgManager.Config

	deleteVolumes, flagErr := cmd.Flags().GetBool("delete-volumes")
	if flagErr != nil {
		return fmt.Errorf("failed to get delete-volumes flag: %w", flagErr)
	}

	err = cleanupMirrorRegistries(cmd, clusterCfg, deps, deleteVolumes)
	if err != nil {
		// Log warning but don't fail the delete operation
		notify.WriteMessage(notify.Message{
			Type:    notify.WarningType,
			Content: fmt.Sprintf("failed to cleanup registries: %v", err),
			Writer:  cmd.OutOrStdout(),
		})
	}

	if clusterCfg.Spec.LocalRegistry == v1alpha1.LocalRegistryEnabled {
		err = cleanupLocalRegistry(cmd, clusterCfg, deps, deleteVolumes)
		if err != nil {
			notify.WriteMessage(notify.Message{
				Type:    notify.WarningType,
				Content: fmt.Sprintf("failed to cleanup local registry: %v", err),
				Writer:  cmd.OutOrStdout(),
			})
		}
	}

	return nil
}

// cleanupMirrorRegistries cleans up registries for Kind after cluster deletion.
// K3d handles registry cleanup natively through its own configuration.
func cleanupMirrorRegistries(
	cmd *cobra.Command,
	clusterCfg *v1alpha1.Cluster,
	deps cmdhelpers.LifecycleDeps,
	deleteVolumes bool,
) error {
	switch clusterCfg.Spec.Distribution {
	case v1alpha1.DistributionKind:
		return cleanupKindMirrorRegistries(cmd, clusterCfg, deps, deleteVolumes)
	case v1alpha1.DistributionK3d:
		return cleanupK3dMirrorRegistries(cmd, clusterCfg, deps, deleteVolumes)
	default:
		return nil
	}
}

func cleanupKindMirrorRegistries(
	cmd *cobra.Command,
	clusterCfg *v1alpha1.Cluster,
	deps cmdhelpers.LifecycleDeps,
	deleteVolumes bool,
) error {
	kindConfigMgr := kindconfigmanager.NewConfigManager(clusterCfg.Spec.DistributionConfig)

	kindConfig, loadErr := kindConfigMgr.LoadConfig(deps.Timer)
	if loadErr != nil {
		return fmt.Errorf("failed to load kind config: %w", loadErr)
	}

	registriesInfo := kindprovisioner.ExtractRegistriesFromKindForTesting(kindConfig, nil)

	registryNames := registry.CollectRegistryNames(registriesInfo)
	if len(registryNames) == 0 {
		return nil
	}

	return runMirrorRegistryCleanup(
		cmd,
		deps,
		registryNames,
		func(dockerClient client.APIClient) error {
			return kindprovisioner.CleanupRegistries(
				cmd.Context(),
				kindConfig,
				kindConfig.Name,
				dockerClient,
				deleteVolumes,
			)
		},
	)
}

func cleanupK3dMirrorRegistries(
	cmd *cobra.Command,
	clusterCfg *v1alpha1.Cluster,
	deps cmdhelpers.LifecycleDeps,
	deleteVolumes bool,
) error {
	if clusterCfg.Spec.DistributionConfig == "" {
		return nil
	}

	k3dConfigMgr := k3dconfigmanager.NewConfigManager(clusterCfg.Spec.DistributionConfig)

	k3dConfig, loadErr := k3dConfigMgr.LoadConfig(deps.Timer)
	if loadErr != nil {
		return fmt.Errorf("failed to load k3d config: %w", loadErr)
	}

	registriesInfo := k3dprovisioner.ExtractRegistriesFromConfigForTesting(k3dConfig)

	registryNames := registry.CollectRegistryNames(registriesInfo)
	if len(registryNames) == 0 {
		return nil
	}

	return runMirrorRegistryCleanup(
		cmd,
		deps,
		registryNames,
		func(dockerClient client.APIClient) error {
			return k3dprovisioner.CleanupRegistries(
				cmd.Context(),
				k3dConfig,
				k3dConfig.Name,
				dockerClient,
				deleteVolumes,
				cmd.ErrOrStderr(),
			)
		},
	)
}

func runMirrorRegistryCleanup(
	cmd *cobra.Command,
	deps cmdhelpers.LifecycleDeps,
	registryNames []string,
	cleanup func(client.APIClient) error,
) error {
	if len(registryNames) == 0 {
		return nil
	}

	deps.Timer.NewStage()

	cmd.Println()
	notify.WriteMessage(notify.Message{
		Type:    notify.TitleType,
		Content: "Delete mirror registry...",
		Emoji:   "🗑️",
		Writer:  cmd.OutOrStdout(),
	})

	dockerClientInvokerMu.RLock()

	invoker := dockerClientInvoker

	dockerClientInvokerMu.RUnlock()

	err := invoker(cmd, func(dockerClient client.APIClient) error {
		return executeRegistryCleanup(cmd, dockerClient, registryNames, cleanup, deps.Timer)
	})
	if err != nil {
		return fmt.Errorf("failed to delete mirror registries: %w", err)
	}

	return nil
}

func executeRegistryCleanup(
	cmd *cobra.Command,
	dockerClient client.APIClient,
	registryNames []string,
	cleanup func(client.APIClient) error,
	tmr timer.Timer,
) error {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	registryMgr, _ := dockerclient.NewRegistryManager(dockerClient)

	err := cleanup(dockerClient)
	if err != nil {
		return fmt.Errorf("failed to cleanup registries: %w", err)
	}

	notifyRegistryDeletions(ctx, cmd, registryNames, registryMgr)

	outputTimer := cmdhelpers.MaybeTimer(cmd, tmr)

	notify.WriteMessage(notify.Message{
		Type:    notify.SuccessType,
		Content: "mirror registries deleted",
		Timer:   outputTimer,
		Writer:  cmd.OutOrStdout(),
	})

	return nil
}

func notifyRegistryDeletions(
	ctx context.Context,
	cmd *cobra.Command,
	registryNames []string,
	registryMgr *dockerclient.RegistryManager,
) {
	for _, name := range registryNames {
		content := "deleting '%s'"

		if registryMgr != nil {
			inUse, checkErr := registryMgr.IsRegistryInUse(ctx, name)
			if checkErr == nil && inUse {
				content = "skipping '%s' as it is in use"
			}
		}

		notify.WriteMessage(notify.Message{
			Type:    notify.ActivityType,
			Content: content,
			Writer:  cmd.OutOrStdout(),
			Args:    []any{name},
		})
	}
}
