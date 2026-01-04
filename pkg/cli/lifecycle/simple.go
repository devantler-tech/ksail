package lifecycle

import (
	"context"
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
	var contextFlag string

	cmd := &cobra.Command{
		Use:          config.Use,
		Short:        config.Short,
		Long:         config.Long,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runSimpleLifecycleAction(cmd, contextFlag, config)
		},
	}

	cmd.Flags().StringVarP(
		&contextFlag,
		"context",
		"c",
		"",
		"Kubernetes context to target (defaults to current context)",
	)

	return cmd
}

func runSimpleLifecycleAction(
	cmd *cobra.Command,
	contextFlag string,
	config SimpleLifecycleConfig,
) error {
	ctx, err := resolveContext(contextFlag)
	if err != nil {
		return err
	}

	distribution, clusterName, err := DetectDistributionFromContext(ctx)
	if err != nil {
		return fmt.Errorf("failed to detect distribution: %w", err)
	}

	_, _ = fmt.Fprintln(cmd.OutOrStdout())
	notify.WriteMessage(notify.Message{
		Type:    notify.TitleType,
		Content: config.TitleContent,
		Emoji:   config.TitleEmoji,
		Writer:  cmd.OutOrStdout(),
	})

	notify.WriteMessage(notify.Message{
		Type:    notify.ActivityType,
		Content: fmt.Sprintf("%s %s cluster '%s'", config.Activity, distribution, clusterName),
		Writer:  cmd.OutOrStdout(),
	})

	provisioner, err := CreateMinimalProvisioner(distribution, clusterName)
	if err != nil {
		return fmt.Errorf("failed to create provisioner: %w", err)
	}

	err = config.Action(cmd.Context(), provisioner, clusterName)
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

	//nolint:gosec // kubeconfigPath is validated from known sources
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

// resolveContext returns the provided context if non-empty, otherwise reads from kubeconfig.
func resolveContext(contextFlag string) (string, error) {
	if contextFlag != "" {
		return contextFlag, nil
	}

	return getCurrentContext()
}

// CreateMinimalProvisioner creates a minimal provisioner for lifecycle operations.
// These provisioners only need enough configuration to identify containers.
//
//nolint:ireturn // Interface return is required for provisioner abstraction
func CreateMinimalProvisioner(
	distribution v1alpha1.Distribution,
	clusterName string,
) (clusterprovisioner.ClusterProvisioner, error) {
	switch distribution {
	case v1alpha1.DistributionKind:
		kindConfig := &v1alpha4.Cluster{Name: clusterName}

		provisioner, err := kindprovisioner.CreateProvisioner(kindConfig, "")
		if err != nil {
			return nil, fmt.Errorf("failed to create kind provisioner: %w", err)
		}

		return provisioner, nil

	case v1alpha1.DistributionK3d:
		k3dConfig := &k3dv1alpha5.SimpleConfig{
			ObjectMeta: k3dtypes.ObjectMeta{Name: clusterName},
		}

		return k3dprovisioner.CreateProvisioner(k3dConfig, ""), nil

	case v1alpha1.DistributionTalos:
		talosConfig := &talosconfigmanager.Configs{Name: clusterName}

		// Simple lifecycle is for start/stop/delete - not cluster creation.
		// skipCNIChecks doesn't matter here since we're not running bootstrap checks.
		provisioner, err := talosprovisioner.CreateProvisioner(
			talosConfig,
			"",
			v1alpha1.OptionsTalos{},
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
			distribution,
		)
	}
}
