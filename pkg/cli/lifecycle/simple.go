package lifecycle

import (
	"context"
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

// SimpleLifecycleConfig defines the configuration for a simple lifecycle command.
// Simple lifecycle commands auto-detect the cluster from the kubeconfig context
// and don't require a ksail.yaml configuration file.
type SimpleLifecycleConfig struct {
	Use          string
	Short        string
	Long         string
	TitleEmoji   string
	TitleContent string
	Activity     string
	Success      string
	Action       func(
		ctx context.Context,
		provisioner clusterprovisioner.ClusterProvisioner,
		clusterName string,
	) error
}

// NewSimpleLifecycleCmd creates a simple lifecycle command (start/stop) with context auto-detection.
// Unlike config-based lifecycle commands, these commands don't require a ksail.yaml file
// and instead detect the cluster from the kubeconfig context pattern.
func NewSimpleLifecycleCmd(config SimpleLifecycleConfig) *cobra.Command {
	var (
		contextFlag    string
		kubeconfigFlag string
	)

	cmd := &cobra.Command{
		Use:          config.Use,
		Short:        config.Short,
		Long:         config.Long,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runSimpleLifecycleAction(cmd, kubeconfigFlag, contextFlag, config)
		},
	}

	cmd.Flags().StringVarP(
		&contextFlag,
		"context",
		"c",
		"",
		"Kubernetes context to target (defaults to current context)",
	)

	cmd.Flags().StringVar(
		&kubeconfigFlag,
		"kubeconfig",
		"",
		"Path to kubeconfig file (defaults to $KUBECONFIG or ~/.kube/config)",
	)

	return cmd
}

func runSimpleLifecycleAction(
	cmd *cobra.Command,
	kubeconfigPath string,
	contextFlag string,
	config SimpleLifecycleConfig,
) error {
	// Wrap output with StageSeparatingWriter for automatic stage separation
	stageWriter := notify.NewStageSeparatingWriter(cmd.OutOrStdout())
	cmd.SetOut(stageWriter)

	// Detect cluster info from kubeconfig
	clusterInfo, err := DetectClusterInfo(kubeconfigPath, contextFlag)
	if err != nil {
		return fmt.Errorf("failed to detect cluster: %w", err)
	}

	notify.WriteMessage(notify.Message{
		Type:    notify.TitleType,
		Content: config.TitleContent,
		Emoji:   config.TitleEmoji,
		Writer:  cmd.OutOrStdout(),
	})

	notify.WriteMessage(notify.Message{
		Type:    notify.ActivityType,
		Content: fmt.Sprintf("%s %s cluster '%s'", config.Activity, clusterInfo.Distribution, clusterInfo.ClusterName),
		Writer:  cmd.OutOrStdout(),
	})

	provisioner, err := CreateMinimalProvisioner(clusterInfo)
	if err != nil {
		return fmt.Errorf("failed to create provisioner: %w", err)
	}

	err = config.Action(cmd.Context(), provisioner, clusterInfo.ClusterName)
	if err != nil {
		return fmt.Errorf("failed to %s cluster: %w", config.Activity, err)
	}

	notify.WriteMessage(notify.Message{
		Type:    notify.SuccessType,
		Content: config.Success,
		Writer:  cmd.OutOrStdout(),
	})

	return nil
}

// GetCurrentKubeContext reads the current context from the default kubeconfig.
// This is exported for use by other commands that need context-based auto-detection.
func GetCurrentKubeContext() (string, error) {
	clusterInfo, err := DetectClusterInfo("", "")
	if err != nil {
		return "", err
	}

	// Reconstruct the context name from the cluster info
	switch clusterInfo.Distribution {
	case v1alpha1.DistributionVanilla:
		return "kind-" + clusterInfo.ClusterName, nil
	case v1alpha1.DistributionK3s:
		return "k3d-" + clusterInfo.ClusterName, nil
	case v1alpha1.DistributionTalos:
		return "admin@" + clusterInfo.ClusterName, nil
	default:
		return "", fmt.Errorf("unknown distribution: %s", clusterInfo.Distribution)
	}
}

// CreateMinimalProvisioner creates a minimal provisioner for lifecycle operations.
// These provisioners only need enough configuration to identify containers.
// It uses the detected ClusterInfo to create the appropriate provisioner
// with the correct provider configuration.
//
//nolint:ireturn // Interface return is required for provisioner abstraction
func CreateMinimalProvisioner(
	info *ClusterInfo,
) (clusterprovisioner.ClusterProvisioner, error) {
	switch info.Distribution {
	case v1alpha1.DistributionVanilla:
		kindConfig := &v1alpha4.Cluster{Name: info.ClusterName}

		provisioner, err := kindprovisioner.CreateProvisioner(kindConfig, "")
		if err != nil {
			return nil, fmt.Errorf("failed to create kind provisioner: %w", err)
		}

		return provisioner, nil

	case v1alpha1.DistributionK3s:
		k3dConfig := &k3dv1alpha5.SimpleConfig{
			ObjectMeta: k3dtypes.ObjectMeta{Name: info.ClusterName},
		}

		return k3dprovisioner.CreateProvisioner(k3dConfig, ""), nil

	case v1alpha1.DistributionTalos:
		talosConfig := &talosconfigmanager.Configs{Name: info.ClusterName}

		// Create provisioner with detected provider and kubeconfig path
		provisioner, err := talosprovisioner.CreateProvisioner(
			talosConfig,
			info.KubeconfigPath,
			info.Provider,
			v1alpha1.OptionsTalos{},
			v1alpha1.OptionsHetzner{},
			false, // skipCNIChecks - not relevant for simple lifecycle operations
		)
		if err != nil {
			return nil, fmt.Errorf("failed to create talos provisioner: %w", err)
		}

		return provisioner, nil

	default:
		return nil, fmt.Errorf(
			"%w: %s",
			clusterprovisioner.ErrUnsupportedDistribution,
			info.Distribution,
		)
	}
}
