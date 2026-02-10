package lifecycle

import (
	"context"
	"errors"
	"fmt"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	configmanager "github.com/devantler-tech/ksail/v5/pkg/fsutil/configmanager"
	ksailconfigmanager "github.com/devantler-tech/ksail/v5/pkg/fsutil/configmanager/ksail"
	talosconfigmanager "github.com/devantler-tech/ksail/v5/pkg/fsutil/configmanager/talos"
	"github.com/devantler-tech/ksail/v5/pkg/notify"
	clusterdetector "github.com/devantler-tech/ksail/v5/pkg/svc/detector/cluster"
	clusterprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster"
	talosprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/talos"
	"github.com/spf13/cobra"
)

// Lifecycle errors.
var (
	// ErrClusterNameRequired indicates that a cluster name is required but was not provided.
	ErrClusterNameRequired = errors.New(
		"cluster name is required: use --name flag, create a ksail.yaml config, or set a kubeconfig context",
	)
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

// NewSimpleLifecycleCmd creates a simple lifecycle command (start/stop) with --name and --provider flags.
// The cluster is resolved in the following priority order:
//  1. From --name flag (required if no config or context available)
//  2. From ksail.yaml config file (if present)
//  3. From current kubeconfig context (if detectable)
//
// The provider is resolved in the following priority order:
//  1. From --provider flag
//  2. From ksail.yaml config file (if present)
//  3. Defaults to Docker
func NewSimpleLifecycleCmd(config SimpleLifecycleConfig) *cobra.Command {
	var (
		nameFlag     string
		providerFlag v1alpha1.Provider
	)

	cmd := &cobra.Command{
		Use:          config.Use,
		Short:        config.Short,
		Long:         config.Long,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runSimpleLifecycleAction(cmd, nameFlag, providerFlag, config)
		},
	}

	cmd.Flags().StringVarP(
		&nameFlag,
		"name",
		"n",
		"",
		"Name of the cluster to target",
	)

	cmd.Flags().VarP(
		&providerFlag,
		"provider",
		"p",
		fmt.Sprintf("Provider to use (%s)", providerFlag.ValidValues()),
	)

	return cmd
}

// ResolvedClusterInfo contains the resolved cluster name, provider, and kubeconfig path.
type ResolvedClusterInfo struct {
	ClusterName    string
	Provider       v1alpha1.Provider
	KubeconfigPath string
}

// ResolveClusterInfo resolves the cluster name, provider, and kubeconfig from flags, config, or kubeconfig.
// Priority for cluster name: flag > config > kubeconfig context.
// Priority for provider: flag > config > default (Docker).
// Priority for kubeconfig: flag > env (KUBECONFIG) > config > default (~/.kube/config).
func ResolveClusterInfo(
	nameFlag string,
	providerFlag v1alpha1.Provider,
	kubeconfigFlag string,
) (*ResolvedClusterInfo, error) {
	clusterName := nameFlag
	provider := providerFlag
	kubeconfigPath := kubeconfigFlag

	// Fill missing values from ksail.yaml config file
	if clusterName == "" {
		resolveFromConfig(&clusterName, &provider, &kubeconfigPath)
	}

	// Fall back to kubeconfig context detection
	if clusterName == "" {
		resolveFromKubecontext(&clusterName, &provider, kubeconfigPath)
	}

	if clusterName == "" {
		return nil, ErrClusterNameRequired
	}

	if provider == "" {
		provider = v1alpha1.ProviderDocker
	}

	resolvedPath, err := clusterdetector.ResolveKubeconfigPath(kubeconfigPath)
	if err != nil {
		return nil, fmt.Errorf("resolve kubeconfig path: %w", err)
	}

	return &ResolvedClusterInfo{
		ClusterName:    clusterName,
		Provider:       provider,
		KubeconfigPath: resolvedPath,
	}, nil
}

// resolveFromConfig fills missing cluster info from the ksail.yaml config file.
func resolveFromConfig(
	clusterName *string,
	provider *v1alpha1.Provider,
	kubeconfigPath *string,
) {
	cfgManager := ksailconfigmanager.NewConfigManager(nil)

	cfg, err := cfgManager.Load(configmanager.LoadOptions{Silent: true, SkipValidation: true})
	if err != nil || cfg == nil || !cfgManager.IsConfigFileFound() {
		return
	}

	*clusterName = clusterNameFromDistConfig(cfgManager.DistributionConfig)

	if *provider == "" && cfg.Spec.Cluster.Provider != "" {
		*provider = cfg.Spec.Cluster.Provider
	}

	if *kubeconfigPath == "" && cfg.Spec.Cluster.Connection.Kubeconfig != "" {
		*kubeconfigPath = cfg.Spec.Cluster.Connection.Kubeconfig
	}
}

// clusterNameFromDistConfig extracts the cluster name from distribution-specific config.
func clusterNameFromDistConfig(distCfg *clusterprovisioner.DistributionConfig) string {
	if distCfg == nil {
		return ""
	}

	switch {
	case distCfg.Kind != nil && distCfg.Kind.Name != "":
		return distCfg.Kind.Name
	case distCfg.K3d != nil && distCfg.K3d.Name != "":
		return distCfg.K3d.Name
	case distCfg.Talos != nil && distCfg.Talos.GetClusterName() != "":
		return distCfg.Talos.GetClusterName()
	default:
		return ""
	}
}

// resolveFromKubecontext fills missing cluster info from the current kubeconfig context.
func resolveFromKubecontext(
	clusterName *string,
	provider *v1alpha1.Provider,
	kubeconfigPath string,
) {
	clusterInfo, err := clusterdetector.DetectInfo(kubeconfigPath, "")
	if err != nil || clusterInfo == nil {
		return
	}

	*clusterName = clusterInfo.ClusterName

	if *provider == "" {
		*provider = clusterInfo.Provider
	}
}

func runSimpleLifecycleAction(
	cmd *cobra.Command,
	nameFlag string,
	providerFlag v1alpha1.Provider,
	config SimpleLifecycleConfig,
) error {
	// Wrap output with StageSeparatingWriter for automatic stage separation
	stageWriter := notify.NewStageSeparatingWriter(cmd.OutOrStdout())
	cmd.SetOut(stageWriter)

	// Resolve cluster info from flags, config, or kubeconfig
	// Empty kubeconfig flag - simple lifecycle commands don't need kubeconfig cleanup
	resolved, err := ResolveClusterInfo(nameFlag, providerFlag, "")
	if err != nil {
		return err
	}

	notify.WriteMessage(notify.Message{
		Type:    notify.TitleType,
		Content: config.TitleContent,
		Emoji:   config.TitleEmoji,
		Writer:  cmd.OutOrStdout(),
	})

	notify.WriteMessage(notify.Message{
		Type: notify.ActivityType,
		Content: fmt.Sprintf(
			"%s cluster '%s' on %s",
			config.Activity,
			resolved.ClusterName,
			resolved.Provider,
		),
		Writer: cmd.OutOrStdout(),
	})

	// Create cluster info for provisioner creation
	clusterInfo := &clusterdetector.Info{
		ClusterName:    resolved.ClusterName,
		Provider:       resolved.Provider,
		KubeconfigPath: resolved.KubeconfigPath,
	}

	provisioner, err := CreateMinimalProvisionerForProvider(clusterInfo)
	if err != nil {
		return fmt.Errorf("failed to create provisioner: %w", err)
	}

	err = config.Action(cmd.Context(), provisioner, resolved.ClusterName)
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

// CreateMinimalProvisioner creates a minimal provisioner for lifecycle operations.
// These provisioners only need enough configuration to identify containers.
// It uses the detected ClusterInfo to create the appropriate provisioner
// with the correct provider configuration.
func CreateMinimalProvisioner(
	info *clusterdetector.Info,
) (clusterprovisioner.ClusterProvisioner, error) {
	provisioner, err := clusterprovisioner.CreateMinimalProvisioner(
		info.Distribution,
		info.ClusterName,
		info.KubeconfigPath,
		info.Provider,
	)
	if err != nil {
		return nil, fmt.Errorf("creating minimal provisioner: %w", err)
	}

	return provisioner, nil
}

// CreateMinimalProvisionerForProvider creates provisioners for all distributions
// that support the given provider, and returns the first one that can operate on the cluster.
// This is used when we only have --name and --provider flags without distribution info.
func CreateMinimalProvisionerForProvider(
	info *clusterdetector.Info,
) (clusterprovisioner.ClusterProvisioner, error) {
	switch info.Provider {
	case v1alpha1.ProviderDocker, "":
		// Docker provider supports all distributions - create a multi-provisioner
		// that tries each distribution in order
		return clusterprovisioner.NewMultiProvisioner(info.ClusterName), nil

	case v1alpha1.ProviderHetzner:
		// Hetzner only supports Talos
		talosConfig := &talosconfigmanager.Configs{Name: info.ClusterName}

		// Use default kubeconfig path for cleanup operations
		kubeconfigPath := info.KubeconfigPath
		if kubeconfigPath == "" {
			kubeconfigPath = "~/.kube/config"
		}

		provisioner, err := talosprovisioner.CreateProvisioner(
			talosConfig,
			kubeconfigPath,
			info.Provider,
			v1alpha1.OptionsTalos{},
			v1alpha1.OptionsHetzner{},
			false,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to create talos provisioner: %w", err)
		}

		return provisioner, nil

	default:
		return nil, fmt.Errorf(
			"%w: %s",
			clusterprovisioner.ErrUnsupportedProvider,
			info.Provider,
		)
	}
}
