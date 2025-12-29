package workload

import (
	"fmt"
	"io"

	v1alpha1 "github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/cli/flags"
	"github.com/devantler-tech/ksail/v5/pkg/cli/ui/notify"
	"github.com/devantler-tech/ksail/v5/pkg/cli/ui/timer"
	ksailconfigmanager "github.com/devantler-tech/ksail/v5/pkg/io/config-manager/ksail"
	"github.com/spf13/cobra"
)

// commandContext holds common command execution context.
type commandContext struct {
	Timer       timer.Timer
	OutputTimer timer.Timer
	ClusterCfg  *v1alpha1.Cluster
}

// initCommandContext initializes common command context (timer, config manager, config loading).
func initCommandContext(cmd *cobra.Command) (*commandContext, error) {
	tmr := timer.New()
	tmr.Start()

	fieldSelectors := ksailconfigmanager.DefaultClusterFieldSelectors()
	cfgManager := ksailconfigmanager.NewCommandConfigManager(cmd, fieldSelectors)
	outputTimer := flags.MaybeTimer(cmd, tmr)

	clusterCfg, err := cfgManager.LoadConfig(outputTimer)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	return &commandContext{
		Timer:       tmr,
		OutputTimer: outputTimer,
		ClusterCfg:  clusterCfg,
	}, nil
}

// writeActivityNotification writes an activity notification message.
func writeActivityNotification(
	content string,
	outputTimer timer.Timer,
	writer io.Writer,
) {
	notify.WriteMessage(notify.Message{
		Type:    notify.ActivityType,
		Content: content,
		Timer:   outputTimer,
		Writer:  writer,
	})
}
