package workload

import (
	"fmt"

	v1alpha1 "github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/cli/helpers"
	"github.com/devantler-tech/ksail/v5/pkg/cli/lifecycle"
	configmanager "github.com/devantler-tech/ksail/v5/pkg/io/config-manager/ksail"
	"github.com/devantler-tech/ksail/v5/pkg/utils/timer"
	"github.com/spf13/cobra"
)

// imageCommandContext holds shared state for image commands.
type imageCommandContext struct {
	Timer       timer.Timer
	OutputTimer timer.Timer
	ClusterCfg  *v1alpha1.Cluster
	ClusterInfo *lifecycle.ClusterInfo
}

// createImageConfigManager creates a config manager for image commands.
// Only includes --context and --kubeconfig flags since image commands
// detect the distribution from the running cluster.
func createImageConfigManager(cmd *cobra.Command) *configmanager.ConfigManager {
	fieldSelectors := []configmanager.FieldSelector[v1alpha1.Cluster]{
		configmanager.DefaultContextFieldSelector(),
		configmanager.DefaultKubeconfigFieldSelector(),
	}

	return configmanager.NewCommandConfigManager(cmd, fieldSelectors)
}

// initImageCommandContext initializes the shared context for image commands.
// It loads the config using the provided config manager, skipping validation
// since image commands detect cluster info from the running cluster.
func initImageCommandContext(
	cmd *cobra.Command,
	cfgManager *configmanager.ConfigManager,
) (*imageCommandContext, error) {
	tmr := timer.New()
	tmr.Start()

	outputTimer := helpers.MaybeTimer(cmd, tmr)

	clusterCfg, err := cfgManager.LoadConfigSilent()
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	return &imageCommandContext{
		Timer:       tmr,
		OutputTimer: outputTimer,
		ClusterCfg:  clusterCfg,
	}, nil
}

// detectClusterInfo detects the cluster info after printing the header.
// This should be called after initImageCommandContext and after printing the title.
func (ctx *imageCommandContext) detectClusterInfo() error {
	ctx.Timer.NewStage()

	clusterInfo, err := lifecycle.DetectClusterInfo(
		ctx.ClusterCfg.Spec.Cluster.Connection.Kubeconfig,
		ctx.ClusterCfg.Spec.Cluster.Connection.Context,
	)
	if err != nil {
		return fmt.Errorf("detect cluster info: %w", err)
	}

	ctx.ClusterInfo = clusterInfo

	return nil
}
