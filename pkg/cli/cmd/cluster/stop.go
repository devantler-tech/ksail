package cluster

import (
	"context"

	"github.com/devantler-tech/ksail/v5/pkg/cli/lifecycle"
	clusterprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster"
	"github.com/spf13/cobra"
)

const stopLongDesc = `Stop a running Kubernetes cluster.

The cluster is resolved in the following priority order:
  1. From --name flag
  2. From ksail.yaml config file (if present)
  3. From current kubeconfig context

The provider is resolved in the following priority order:
  1. From --provider flag
  2. From ksail.yaml config file (if present)
  3. Defaults to Docker

Supported distributions are automatically detected from existing clusters.`

// NewStopCmd creates and returns the stop command.
func NewStopCmd(_ any) *cobra.Command {
	return lifecycle.NewSimpleLifecycleCmd(lifecycle.SimpleLifecycleConfig{
		Use:          "stop",
		Short:        "Stop a running cluster",
		Long:         stopLongDesc,
		TitleEmoji:   "🛑",
		TitleContent: "Stop cluster...",
		Activity:     "stopping",
		Success:      "cluster stopped",
		Action: func(
			ctx context.Context,
			provisioner clusterprovisioner.ClusterProvisioner,
			clusterName string,
		) error {
			return provisioner.Stop(ctx, clusterName)
		},
	})
}
