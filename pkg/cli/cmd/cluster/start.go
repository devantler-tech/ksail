package cluster

import (
	"context"

	"github.com/devantler-tech/ksail/v5/pkg/cli/annotations"
	"github.com/devantler-tech/ksail/v5/pkg/cli/lifecycle"
	clusterprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster"
	"github.com/spf13/cobra"
)

const startLongDesc = `Start a previously stopped Kubernetes cluster.

The cluster is resolved in the following priority order:
  1. From --name flag
  2. From ksail.yaml config file (if present)
  3. From current kubeconfig context

The provider is resolved in the following priority order:
  1. From --provider flag
  2. From ksail.yaml config file (if present)
  3. Defaults to Docker

Supported distributions are automatically detected from existing clusters.`

// NewStartCmd creates and returns the start command.
func NewStartCmd(_ any) *cobra.Command {
	cmd := lifecycle.NewSimpleLifecycleCmd(lifecycle.SimpleLifecycleConfig{
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

	cmd.Annotations = map[string]string{
		annotations.AnnotationPermission: "write",
	}

	return cmd
}
