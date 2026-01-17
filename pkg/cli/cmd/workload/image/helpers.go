package image

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

// initImageCommandContext initializes the shared context for image commands.
// It loads the config and detects cluster information.
func initImageCommandContext(cmd *cobra.Command) (*imageCommandContext, error) {
	tmr := timer.New()
	tmr.Start()

	fieldSelectors := configmanager.DefaultClusterFieldSelectors()
	cfgManager := configmanager.NewCommandConfigManager(cmd, fieldSelectors)
	outputTimer := helpers.MaybeTimer(cmd, tmr)

	clusterCfg, err := cfgManager.LoadConfig(outputTimer)
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
