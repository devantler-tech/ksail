package cluster

import (
	"context"

	clusterprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster"
	"github.com/spf13/cobra"
)

const startLongDesc = `Start a previously stopped Kubernetes cluster.

The cluster is detected from the provided context or the current kubeconfig context.
Supported distributions are automatically detected:
  - Kind clusters (context pattern: kind-<cluster-name>)
  - K3d clusters (context pattern: k3d-<cluster-name>)
  - Talos clusters (context pattern: admin@<cluster-name>)`

// NewStartCmd creates and returns the start command.
func NewStartCmd(_ any) *cobra.Command {
	return NewSimpleLifecycleCmd(SimpleLifecycleConfig{
		Use:          "start",
		Short:        "Start a stopped cluster",
		Long:         startLongDesc,
		TitleEmoji:   "▶️",
		TitleContent: "Start cluster...",
		Activity:     "starting",
		Success:      "cluster started",
		Action: func(
			ctx context.Context,
			provisioner clusterprovisioner.ClusterProvisioner,
			clusterName string,
		) error {
			return provisioner.Start(ctx, clusterName)
		},
	})
}
