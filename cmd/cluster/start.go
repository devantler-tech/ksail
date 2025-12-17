package cluster

import (
	"context"
	"fmt"

	v1alpha1 "github.com/devantler-tech/ksail-go/pkg/apis/cluster/v1alpha1"
	cmdhelpers "github.com/devantler-tech/ksail-go/pkg/cmd"
	runtime "github.com/devantler-tech/ksail-go/pkg/di"
	ksailconfigmanager "github.com/devantler-tech/ksail-go/pkg/io/config-manager/ksail"
	clusterprovisioner "github.com/devantler-tech/ksail-go/pkg/svc/provisioner/cluster"
	"github.com/spf13/cobra"
)

// newStartLifecycleConfig creates the lifecycle configuration for cluster start.
func newStartLifecycleConfig() cmdhelpers.LifecycleConfig {
	return cmdhelpers.LifecycleConfig{
		TitleEmoji:         "▶️",
		TitleContent:       "Start cluster...",
		ActivityContent:    "starting cluster",
		SuccessContent:     "cluster started",
		ErrorMessagePrefix: "failed to start cluster",
		Action: func(ctx context.Context, provisioner clusterprovisioner.ClusterProvisioner, clusterName string) error {
			return provisioner.Start(ctx, clusterName)
		},
	}
}

// NewStartCmd creates and returns the start command.
func NewStartCmd(runtimeContainer *runtime.Runtime) *cobra.Command {
	cmd := &cobra.Command{
		Use:          "start",
		Short:        "Start a stopped cluster",
		Long:         `Start a previously stopped cluster.`,
		SilenceUsage: true,
	}

	cfgManager := ksailconfigmanager.NewCommandConfigManager(
		cmd,
		ksailconfigmanager.DefaultClusterFieldSelectors(),
	)

	cmd.RunE = cmdhelpers.WrapLifecycleHandler(runtimeContainer, cfgManager, handleStartRunE)

	return cmd
}

func handleStartRunE(
	cmd *cobra.Command,
	cfgManager *ksailconfigmanager.ConfigManager,
	deps cmdhelpers.LifecycleDeps,
) error {
	config := newStartLifecycleConfig()

	err := cmdhelpers.HandleLifecycleRunE(cmd, cfgManager, deps, config)
	if err != nil {
		return fmt.Errorf("start cluster lifecycle: %w", err)
	}

	clusterCfg := cfgManager.Config
	if clusterCfg == nil || clusterCfg.Spec.LocalRegistry != v1alpha1.LocalRegistryEnabled {
		return nil
	}

	kindConfig, k3dConfig, err := loadDistributionConfigs(clusterCfg, deps.Timer)
	if err != nil {
		return fmt.Errorf("load distribution configs: %w", err)
	}

	// Start command's registry connection happens after cluster start, so use a dummy tracker
	dummyTracker := true

	connectErr := executeLocalRegistryStage(
		cmd,
		clusterCfg,
		deps,
		kindConfig,
		k3dConfig,
		localRegistryStageConnect,
		&dummyTracker,
	)
	if connectErr != nil {
		return fmt.Errorf("connect local registry: %w", connectErr)
	}

	return nil
}
