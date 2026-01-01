package cluster

import (
	"errors"
	"fmt"
	"os"
	"strings"

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
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

// ErrNoCurrentContext is returned when no current context is set in kubeconfig.
var ErrNoCurrentContext = errors.New("no current context set in kubeconfig")

// ErrUnknownDistribution is returned when the distribution cannot be detected from context.
var ErrUnknownDistribution = errors.New("unknown distribution: context does not match kind-, k3d-, or admin@ pattern")

// NewStopCmd creates and returns the stop command.
// The stop command auto-detects the cluster from the current kubeconfig context.
func NewStopCmd(_ interface{}) *cobra.Command {
	var contextFlag string

	cmd := &cobra.Command{
		Use:   "stop",
		Short: "Stop a running cluster",
		Long: `Stop a running Kubernetes cluster.

The cluster is detected from the provided context or the current kubeconfig context.
Supported distributions are automatically detected:
  - Kind clusters (context pattern: kind-<cluster-name>)
  - K3d clusters (context pattern: k3d-<cluster-name>)
  - Talos clusters (context pattern: admin@<cluster-name>)`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStop(cmd, contextFlag)
		},
	}

	cmd.Flags().StringVarP(&contextFlag, "context", "c", "", "Kubernetes context to target (defaults to current context)")

	return cmd
}

func runStop(cmd *cobra.Command, contextFlag string) error {
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
		Content: "Stop cluster...",
		Emoji:   "🛑",
		Writer:  cmd.OutOrStdout(),
	})

	notify.WriteMessage(notify.Message{
		Type:    notify.ActivityType,
		Content: fmt.Sprintf("stopping %s cluster '%s'", distribution, clusterName),
		Writer:  cmd.OutOrStdout(),
	})

	// Create minimal provisioner for the detected distribution
	provisioner, err := createMinimalProvisioner(distribution, clusterName)
	if err != nil {
		return fmt.Errorf("failed to create provisioner: %w", err)
	}

	// Stop the cluster
	err = provisioner.Stop(cmd.Context(), clusterName)
	if err != nil {
		return fmt.Errorf("failed to stop cluster: %w", err)
	}

	notify.WriteMessage(notify.Message{
		Type:    notify.SuccessType,
		Content: "cluster stopped",
		Writer:  cmd.OutOrStdout(),
	})

	return nil
}

// getCurrentContext reads the current context from the default kubeconfig.
func getCurrentContext() (string, error) {
	kubeconfigPath := clientcmd.RecommendedHomeFile

	// Check if KUBECONFIG env var is set
	if envPath := os.Getenv("KUBECONFIG"); envPath != "" {
		// Use first path if multiple are specified
		paths := strings.Split(envPath, string(os.PathListSeparator))
		if len(paths) > 0 && paths[0] != "" {
			kubeconfigPath = paths[0]
		}
	}

	configBytes, err := os.ReadFile(kubeconfigPath)
	if err != nil {
		return "", fmt.Errorf("failed to read kubeconfig: %w", err)
	}

	config, err := clientcmd.Load(configBytes)
	if err != nil {
		return "", fmt.Errorf("failed to parse kubeconfig: %w", err)
	}

	if config.CurrentContext == "" {
		return "", ErrNoCurrentContext
	}

	return config.CurrentContext, nil
}

// detectDistributionFromContext detects the distribution and cluster name from a context string.
func detectDistributionFromContext(context string) (v1alpha1.Distribution, string, error) {
	// Kind: kind-<cluster-name>
	if clusterName, ok := strings.CutPrefix(context, "kind-"); ok {
		return v1alpha1.DistributionKind, clusterName, nil
	}

	// K3d: k3d-<cluster-name>
	if clusterName, ok := strings.CutPrefix(context, "k3d-"); ok {
		return v1alpha1.DistributionK3d, clusterName, nil
	}

	// Talos: admin@<cluster-name>
	if clusterName, ok := strings.CutPrefix(context, "admin@"); ok {
		return v1alpha1.DistributionTalos, clusterName, nil
	}

	return "", "", fmt.Errorf("%w: %s", ErrUnknownDistribution, context)
}

// createMinimalProvisioner creates a minimal provisioner for stop operations.
// These provisioners only need enough configuration to identify and stop containers.
func createMinimalProvisioner(distribution v1alpha1.Distribution, clusterName string) (clusterprovisioner.ClusterProvisioner, error) {
	switch distribution {
	case v1alpha1.DistributionKind:
		// Kind provisioner needs minimal config with cluster name
		kindConfig := &v1alpha4.Cluster{Name: clusterName}
		return kindprovisioner.CreateProvisioner(kindConfig, "")

	case v1alpha1.DistributionK3d:
		// K3d provisioner uses Cobra commands, minimal config is sufficient
		k3dConfig := &k3dv1alpha5.SimpleConfig{
			ObjectMeta: k3dtypes.ObjectMeta{Name: clusterName},
		}
		return k3dprovisioner.CreateProvisioner(k3dConfig, ""), nil

	case v1alpha1.DistributionTalos:
		// Talos provisioner needs minimal config with cluster name
		talosConfig := &talosconfigmanager.Configs{Name: clusterName}
		return talosprovisioner.CreateProvisioner(talosConfig, "", v1alpha1.OptionsTalos{})

	default:
		return nil, fmt.Errorf("%w: %s", clusterprovisioner.ErrUnsupportedDistribution, distribution)
	}
}
