package cluster

import (
	"fmt"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	talosconfigmanager "github.com/devantler-tech/ksail/v5/pkg/io/config-manager/talos"
	clusterprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster"
	k3dprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/k3d"
	kindprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/kind"
	talosprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/talos"
	"github.com/devantler-tech/ksail/v5/pkg/utils/notify"
	k3dtypes "github.com/k3d-io/k3d/v5/pkg/config/types"
	k3dv1alpha5 "github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
	"github.com/spf13/cobra"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

// NewStartCmd creates and returns the start command.
// The start command auto-detects the cluster from the current kubeconfig context.
func NewStartCmd(_ interface{}) *cobra.Command {
	var contextFlag string

	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start a stopped cluster",
		Long: `Start a previously stopped Kubernetes cluster.

The cluster is detected from the provided context or the current kubeconfig context.
Supported distributions are automatically detected:
  - Kind clusters (context pattern: kind-<cluster-name>)
  - K3d clusters (context pattern: k3d-<cluster-name>)
  - Talos clusters (context pattern: admin@<cluster-name>)`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStart(cmd, contextFlag)
		},
	}

	cmd.Flags().StringVarP(&contextFlag, "context", "c", "", "Kubernetes context to target (defaults to current context)")

	return cmd
}

func runStart(cmd *cobra.Command, contextFlag string) error {
	// Use provided context or get current context from kubeconfig
	context := contextFlag
	if context == "" {
		var err error
		context, err = getCurrentContext()
		if err != nil {
			return err
		}
	}

	// Detect distribution and extract cluster name from context
	distribution, clusterName, err := detectDistributionFromContext(context)
	if err != nil {
		return err
	}

	// Show title
	_, _ = fmt.Fprintln(cmd.OutOrStdout())
	notify.WriteMessage(notify.Message{
		Type:    notify.TitleType,
		Content: "Start cluster...",
		Emoji:   "▶️",
		Writer:  cmd.OutOrStdout(),
	})

	notify.WriteMessage(notify.Message{
		Type:    notify.ActivityType,
		Content: fmt.Sprintf("starting %s cluster '%s'", distribution, clusterName),
		Writer:  cmd.OutOrStdout(),
	})

	// Create minimal provisioner for the detected distribution
	provisioner, err := createMinimalProvisioner(distribution, clusterName)
	if err != nil {
		return fmt.Errorf("failed to create provisioner: %w", err)
	}

	// Start the cluster
	err = provisioner.Start(cmd.Context(), clusterName)
	if err != nil {
		return fmt.Errorf("failed to start cluster: %w", err)
	}

	notify.WriteMessage(notify.Message{
		Type:    notify.SuccessType,
		Content: "cluster started",
		Writer:  cmd.OutOrStdout(),
	})

	return nil
}

// createMinimalProvisionerForStart creates a minimal provisioner for start operations.
// These provisioners only need enough configuration to identify and start containers.
func createMinimalProvisionerForStart(distribution v1alpha1.Distribution, clusterName string) (clusterprovisioner.ClusterProvisioner, error) {
	switch distribution {
	case v1alpha1.DistributionKind:
		kindConfig := &v1alpha4.Cluster{Name: clusterName}
		return kindprovisioner.CreateProvisioner(kindConfig, "")

	case v1alpha1.DistributionK3d:
		k3dConfig := &k3dv1alpha5.SimpleConfig{
			ObjectMeta: k3dtypes.ObjectMeta{Name: clusterName},
		}
		return k3dprovisioner.CreateProvisioner(k3dConfig, ""), nil

	case v1alpha1.DistributionTalos:
		talosConfig := &talosconfigmanager.Configs{Name: clusterName}
		return talosprovisioner.CreateProvisioner(talosConfig, "", v1alpha1.OptionsTalos{})

	default:
		return nil, fmt.Errorf("%w: %s", clusterprovisioner.ErrUnsupportedDistribution, distribution)
	}
}
