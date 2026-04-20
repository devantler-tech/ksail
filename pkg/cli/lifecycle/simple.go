package lifecycle

import (
	"context"
	"errors"
	"fmt"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/flags"
	configmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager"
	ksailconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/ksail"
	talosconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/talos"
	"github.com/devantler-tech/ksail/v7/pkg/notify"
	clusterdetector "github.com/devantler-tech/ksail/v7/pkg/svc/detector/cluster"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	talosprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/talos"
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
		provisioner clusterprovisioner.Provisioner,
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
	OmniOpts       v1alpha1.OptionsOmni
}

// ResolveClusterInfo resolves the cluster name, provider, and kubeconfig from flags, config, or kubeconfig.
// Priority for cluster name: flag > config > kubeconfig context.
// Priority for provider: flag > config > default (Docker).
// Priority for kubeconfig: flag > env (KUBECONFIG) > config > default (~/.kube/config).
//
// When cmd is non-nil, the --config persistent flag is honored for config loading.
func ResolveClusterInfo(
	cmd *cobra.Command,
	nameFlag string,
	providerFlag v1alpha1.Provider,
	kubeconfigFlag string,
) (*ResolvedClusterInfo, error) {
	clusterName := nameFlag
	provider := providerFlag
	kubeconfigPath := kubeconfigFlag

	// Always load config to fill missing fields and extract Omni options.
	// Even when --name is provided, we still need Omni endpoint from config.
	var omniOpts v1alpha1.OptionsOmni

	resolveFromConfig(cmd, &clusterName, &provider, &kubeconfigPath, &omniOpts)

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
		OmniOpts:       omniOpts,
	}, nil
}

// loadConfig loads the ksail.yaml config, honoring the --config flag when cmd is non-nil.
// Returns nil if no config is found or loading fails.
func loadConfig(cmd *cobra.Command) (*v1alpha1.Cluster, *clusterprovisioner.DistributionConfig) {
	var configFile string

	if cmd != nil {
		cfgPath, err := flags.GetConfigPath(cmd)
		if err == nil {
			configFile = cfgPath
		}
	}

	cfgManager := ksailconfigmanager.NewConfigManager(nil, configFile)

	cfg, err := cfgManager.Load(configmanager.LoadOptions{Silent: true, SkipValidation: true})
	if err != nil || cfg == nil || !cfgManager.IsConfigFileFound() {
		return nil, nil
	}

	return cfg, cfgManager.DistributionConfig
}

// resolveFromConfig fills missing cluster info and Omni options from the ksail.yaml config.
// When cmd is non-nil, the --config persistent flag is honored.
// Fields that already have values (from flags) are not overwritten.
func resolveFromConfig(
	cmd *cobra.Command,
	clusterName *string,
	provider *v1alpha1.Provider,
	kubeconfigPath *string,
	omniOpts *v1alpha1.OptionsOmni,
) {
	cfg, distCfg := loadConfig(cmd)
	if cfg == nil {
		return
	}

	if *clusterName == "" {
		*clusterName = clusterNameFromDistConfig(distCfg)
	}

	if *provider == "" && cfg.Spec.Cluster.Provider != "" {
		*provider = cfg.Spec.Cluster.Provider
	}

	if *kubeconfigPath == "" && cfg.Spec.Cluster.Connection.Kubeconfig != "" {
		*kubeconfigPath = cfg.Spec.Cluster.Connection.Kubeconfig
	}

	*omniOpts = cfg.Spec.Provider.Omni
}

// clusterNameFromDistConfig extracts the cluster name from distribution-specific config.
func clusterNameFromDistConfig(distCfg *clusterprovisioner.DistributionConfig) string {
	if distCfg == nil {
		return ""
	}

	if distCfg.Kind != nil {
		return distCfg.Kind.Name
	}

	if distCfg.K3d != nil {
		return distCfg.K3d.Name
	}

	if distCfg.Talos != nil {
		return distCfg.Talos.GetClusterName()
	}

	if distCfg.VCluster != nil {
		return distCfg.VCluster.Name
	}

	if distCfg.KWOK != nil {
		return distCfg.KWOK.Name
	}

	return ""
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
	resolved, err := ResolveClusterInfo(cmd, nameFlag, providerFlag, "")
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

	provisioner, err := CreateMinimalProvisionerForProvider(clusterInfo, resolved.OmniOpts)
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
) (clusterprovisioner.Provisioner, error) {
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
	omniOpts v1alpha1.OptionsOmni,
) (clusterprovisioner.Provisioner, error) {
	switch info.Provider {
	case v1alpha1.ProviderDocker, "":
		// Docker provider supports all distributions - create a multi-provisioner
		// that tries each distribution in order
		return clusterprovisioner.NewMultiProvisioner(info.ClusterName), nil

	case v1alpha1.ProviderHetzner, v1alpha1.ProviderOmni:
		// Hetzner and Omni only support Talos
		talosConfig := &talosconfigmanager.Configs{Name: info.ClusterName}

		// Use default kubeconfig path for cleanup operations
		kubeconfigPath := info.KubeconfigPath
		if kubeconfigPath == "" {
			kubeconfigPath = "~/.kube/config"
		}

		provisioner, err := talosprovisioner.CreateProvisioner(
			talosConfig,
			kubeconfigPath,
			"",
			info.Provider,
			v1alpha1.OptionsTalos{},
			v1alpha1.OptionsHetzner{},
			omniOpts,
			false,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to create talos provisioner: %w", err)
		}

		return provisioner, nil

	case v1alpha1.ProviderAWS:
		// AWS only supports EKS, which is not a docker-based distribution
		// and therefore cannot be produced by the minimal multi-provisioner
		// path. EKS lifecycle operations go through the factory-driven path.
		return nil, fmt.Errorf(
			"%w: AWS provider is only supported via the EKS distribution",
			clusterprovisioner.ErrUnsupportedProvider,
		)

	default:
		return nil, fmt.Errorf(
			"%w: %s",
			clusterprovisioner.ErrUnsupportedProvider,
			info.Provider,
		)
	}
}
