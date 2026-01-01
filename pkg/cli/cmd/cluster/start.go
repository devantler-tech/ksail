package cluster

import (
	"context"
	"fmt"

	v1alpha1 "github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/cli/create/registrystage"
	"github.com/devantler-tech/ksail/v5/pkg/cli/lifecycle"
	runtime "github.com/devantler-tech/ksail/v5/pkg/di"
	ksailconfigmanager "github.com/devantler-tech/ksail/v5/pkg/io/config-manager/ksail"
	clusterprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster"
	"github.com/spf13/cobra"
)

// newStartLifecycleConfig creates the lifecycle configuration for cluster start.
func newStartLifecycleConfig() lifecycle.Config {
	return lifecycle.Config{
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

	cmd.RunE = lifecycle.WrapHandler(runtimeContainer, cfgManager, handleStartRunE)

	return cmd
}

func handleStartRunE(
	cmd *cobra.Command,
	cfgManager *ksailconfigmanager.ConfigManager,
	deps lifecycle.Deps,
) error {
	config := newStartLifecycleConfig()

	err := lifecycle.HandleRunE(cmd, cfgManager, deps, config)
	if err != nil {
		return fmt.Errorf("start cluster lifecycle: %w", err)
	}

	clusterCfg := cfgManager.Config
	if clusterCfg == nil || clusterCfg.Spec.Cluster.LocalRegistry != v1alpha1.LocalRegistryEnabled {
		return nil
	}

	// Create cluster command context
	ctx := NewClusterCommandContext(cfgManager)

	// Start command's registry connection happens after cluster start, so use a dummy tracker
	dummyTracker := true

	connectErr := registrystage.RunLocalRegistryStage(
		cmd,
		ctx.ClusterCfg,
		deps,
		ctx.KindConfig,
		ctx.K3dConfig,
		ctx.TalosConfig,
		registrystage.LocalRegistryConnect,
		&dummyTracker,
		registrystage.DefaultLocalRegistryDependencies(),
	)
	if connectErr != nil {
		return fmt.Errorf("connect local registry: %w", connectErr)
	}

	return nil
}
