package cluster

import (
	"context"

	"github.com/devantler-tech/ksail/v5/pkg/cli/lifecycle"
	clusterprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster"
	"github.com/spf13/cobra"
)

const stopLongDesc = `Stop a running Kubernetes cluster.

The cluster is detected from the provided context or the current kubeconfig context.
Supported distributions are automatically detected:
  - Vanilla (Kind) clusters (context pattern: kind-<cluster-name>)
  - K3s (K3d) clusters (context pattern: k3d-<cluster-name>)
  - Talos clusters (context pattern: admin@<cluster-name>)`

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
