package cluster

import (
	"context"

	"github.com/devantler-tech/ksail/v7/pkg/cli/annotations"
	"github.com/devantler-tech/ksail/v7/pkg/cli/lifecycle"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	"github.com/spf13/cobra"
)

// clusterProviderResolutionDesc documents the shared --name/--provider resolution priority order
// used by both the start and stop long descriptions below (they differ only in their opening line).
const clusterProviderResolutionDesc = `
The cluster is resolved in the following priority order:
  1. From --name flag
  2. From ksail.yaml config file (if present)
  3. From current kubeconfig context

The provider is resolved in the following priority order:
  1. From --provider flag
  2. From ksail.yaml config file (if present)
  3. Defaults to Docker

Supported distributions are automatically detected from existing clusters.`

const startLongDesc = "Start a previously stopped Kubernetes cluster.\n" + clusterProviderResolutionDesc

// NewStartCmd creates and returns the start command.
func NewStartCmd() *cobra.Command {
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
			provisioner clusterprovisioner.Provisioner,
			clusterName string,
		) error {
			return provisioner.Start(ctx, clusterName)
		},
		Guard: unmanagedClusterGuard,
	})

	cmd.Annotations = map[string]string{
		annotations.AnnotationPermission: permissionWrite,
	}

	return cmd
}

const stopLongDesc = "Stop a running Kubernetes cluster.\n" + clusterProviderResolutionDesc

// NewStopCmd creates and returns the stop command.
func NewStopCmd() *cobra.Command {
	cmd := lifecycle.NewSimpleLifecycleCmd(lifecycle.SimpleLifecycleConfig{
		Use:          "stop",
		Short:        "Stop a running cluster",
		Long:         stopLongDesc,
		TitleEmoji:   "🛑",
		TitleContent: "Stop cluster...",
		Activity:     "stopping",
		Success:      "cluster stopped",
		Action: func(
			ctx context.Context,
			provisioner clusterprovisioner.Provisioner,
			clusterName string,
		) error {
			return provisioner.Stop(ctx, clusterName)
		},
		Guard: unmanagedClusterGuard,
	})

	cmd.Annotations = map[string]string{
		annotations.AnnotationPermission: permissionWrite,
	}

	return cmd
}
