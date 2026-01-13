package lifecycle

import (
	"context"
	"errors"
	"fmt"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	ksailconfigmanager "github.com/devantler-tech/ksail/v5/pkg/io/config-manager/ksail"
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

// ErrClusterNameRequired indicates that a cluster name is required but was not provided.
var ErrClusterNameRequired = errors.New(
	"cluster name is required: use --name flag, create a ksail.yaml config, or set a kubeconfig context",
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

// ResolvedClusterInfo contains the resolved cluster name and provider.
type ResolvedClusterInfo struct {
	ClusterName string
	Provider    v1alpha1.Provider
}

// ResolveClusterInfo resolves the cluster name and provider from flags, config, or kubeconfig.
// Priority for cluster name: flag > config > kubeconfig context
// Priority for provider: flag > config > default (Docker)
func ResolveClusterInfo(nameFlag string, providerFlag v1alpha1.Provider) (*ResolvedClusterInfo, error) {
	var clusterName string

	var provider v1alpha1.Provider

	// Try to get values from flags first
	if nameFlag != "" {
		clusterName = nameFlag
	}

	if providerFlag != "" {
		provider = providerFlag
	}

	// If we don't have a cluster name yet, try loading from ksail.yaml config
	if clusterName == "" {
		cfgManager := ksailconfigmanager.NewConfigManager(nil)
		cfg, err := cfgManager.LoadConfigSilent()

		if err == nil && cfg != nil {
			// Get cluster name from config
			if cfgManager.DistributionConfig != nil {
				switch distCfg := cfgManager.DistributionConfig.Active().(type) {
				case *v1alpha4.Cluster:
					if distCfg.Name != "" {
						clusterName = distCfg.Name
					}
				case *k3dv1alpha5.SimpleConfig:
					if distCfg.Name != "" {
						clusterName = distCfg.Name
					}
				case *talosconfigmanager.Configs:
					if distCfg.Name != "" {
						clusterName = distCfg.Name
					}
				}
			}

			// Get provider from config if not set by flag
			if provider == "" && cfg.Spec.Cluster.Provider != "" {
				provider = cfg.Spec.Cluster.Provider
			}
		}
	}

	// If we still don't have a cluster name, try detecting from kubeconfig context
	if clusterName == "" {
		clusterInfo, err := DetectClusterInfo("", "")
		if err == nil && clusterInfo != nil {
			clusterName = clusterInfo.ClusterName

			// Also use detected provider if not set
			if provider == "" {
				provider = clusterInfo.Provider
			}
		}
	}

	// Cluster name is required
	if clusterName == "" {
		return nil, ErrClusterNameRequired
	}

	// Default provider to Docker
	if provider == "" {
		provider = v1alpha1.ProviderDocker
	}

	return &ResolvedClusterInfo{
		ClusterName: clusterName,
		Provider:    provider,
	}, nil
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
	resolved, err := ResolveClusterInfo(nameFlag, providerFlag)
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
	clusterInfo := &ClusterInfo{
		ClusterName: resolved.ClusterName,
		Provider:    resolved.Provider,
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

// CreateMinimalProvisionerForProvider creates provisioners for all distributions
// that support the given provider, and returns the first one that can operate on the cluster.
// This is used when we only have --name and --provider flags without distribution info.
//
//nolint:ireturn // Interface return is required for provisioner abstraction
func CreateMinimalProvisionerForProvider(
	info *ClusterInfo,
) (clusterprovisioner.ClusterProvisioner, error) {
	switch info.Provider {
	case v1alpha1.ProviderDocker, "":
		// Docker provider supports all distributions - create a multi-provisioner
		// that tries each distribution in order
		return createDockerMultiProvisioner(info.ClusterName)

	case v1alpha1.ProviderHetzner:
		// Hetzner only supports Talos
		talosConfig := &talosconfigmanager.Configs{Name: info.ClusterName}

		provisioner, err := talosprovisioner.CreateProvisioner(
			talosConfig,
			"", // kubeconfig path not needed for lifecycle operations
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

// createDockerMultiProvisioner creates a provisioner that tries multiple distributions.
//
//nolint:ireturn // Interface return is required for provisioner abstraction
func createDockerMultiProvisioner(clusterName string) (clusterprovisioner.ClusterProvisioner, error) {
	return &multiDistributionProvisioner{
		clusterName: clusterName,
	}, nil
}

// multiDistributionProvisioner wraps multiple provisioners and routes operations
// to the appropriate one based on which cluster exists.
type multiDistributionProvisioner struct {
	clusterName string
}

// Start starts the cluster by trying each distribution's provisioner.
func (m *multiDistributionProvisioner) Start(ctx context.Context, name string) error {
	clusterName := name
	if clusterName == "" {
		clusterName = m.clusterName
	}

	// Try each distribution in order
	distributions := []v1alpha1.Distribution{
		v1alpha1.DistributionVanilla,
		v1alpha1.DistributionK3s,
		v1alpha1.DistributionTalos,
	}

	for _, dist := range distributions {
		provisioner, err := createProvisionerForDistribution(dist, clusterName)
		if err != nil {
			continue
		}

		exists, err := provisioner.Exists(ctx, clusterName)
		if err != nil {
			continue
		}

		if exists {
			return provisioner.Start(ctx, clusterName)
		}
	}

	return fmt.Errorf("cluster %q not found in any distribution", clusterName)
}

// Stop stops the cluster by trying each distribution's provisioner.
func (m *multiDistributionProvisioner) Stop(ctx context.Context, name string) error {
	clusterName := name
	if clusterName == "" {
		clusterName = m.clusterName
	}

	distributions := []v1alpha1.Distribution{
		v1alpha1.DistributionVanilla,
		v1alpha1.DistributionK3s,
		v1alpha1.DistributionTalos,
	}

	for _, dist := range distributions {
		provisioner, err := createProvisionerForDistribution(dist, clusterName)
		if err != nil {
			continue
		}

		exists, err := provisioner.Exists(ctx, clusterName)
		if err != nil {
			continue
		}

		if exists {
			return provisioner.Stop(ctx, clusterName)
		}
	}

	return fmt.Errorf("cluster %q not found in any distribution", clusterName)
}

// Delete deletes the cluster by trying each distribution's provisioner.
func (m *multiDistributionProvisioner) Delete(ctx context.Context, name string) error {
	clusterName := name
	if clusterName == "" {
		clusterName = m.clusterName
	}

	distributions := []v1alpha1.Distribution{
		v1alpha1.DistributionVanilla,
		v1alpha1.DistributionK3s,
		v1alpha1.DistributionTalos,
	}

	for _, dist := range distributions {
		provisioner, err := createProvisionerForDistribution(dist, clusterName)
		if err != nil {
			continue
		}

		exists, err := provisioner.Exists(ctx, clusterName)
		if err != nil {
			continue
		}

		if exists {
			return provisioner.Delete(ctx, clusterName)
		}
	}

	return fmt.Errorf("cluster %q not found in any distribution", clusterName)
}

// Exists checks if the cluster exists in any distribution.
func (m *multiDistributionProvisioner) Exists(ctx context.Context, name string) (bool, error) {
	clusterName := name
	if clusterName == "" {
		clusterName = m.clusterName
	}

	distributions := []v1alpha1.Distribution{
		v1alpha1.DistributionVanilla,
		v1alpha1.DistributionK3s,
		v1alpha1.DistributionTalos,
	}

	for _, dist := range distributions {
		provisioner, err := createProvisionerForDistribution(dist, clusterName)
		if err != nil {
			continue
		}

		exists, err := provisioner.Exists(ctx, clusterName)
		if err != nil {
			continue
		}

		if exists {
			return true, nil
		}
	}

	return false, nil
}

// List lists all clusters across all distributions.
func (m *multiDistributionProvisioner) List(ctx context.Context) ([]string, error) {
	var allClusters []string

	distributions := []v1alpha1.Distribution{
		v1alpha1.DistributionVanilla,
		v1alpha1.DistributionK3s,
		v1alpha1.DistributionTalos,
	}

	for _, dist := range distributions {
		provisioner, err := createProvisionerForDistribution(dist, m.clusterName)
		if err != nil {
			continue
		}

		clusters, err := provisioner.List(ctx)
		if err != nil {
			continue
		}

		allClusters = append(allClusters, clusters...)
	}

	return allClusters, nil
}

// Create is not supported by the multi-distribution provisioner.
func (m *multiDistributionProvisioner) Create(_ context.Context, _ string) error {
	return errors.New("create not supported without specifying distribution")
}

// createProvisionerForDistribution creates a provisioner for a specific distribution.
//
//nolint:ireturn // Interface return is required for provisioner abstraction
func createProvisionerForDistribution(
	dist v1alpha1.Distribution,
	clusterName string,
) (clusterprovisioner.ClusterProvisioner, error) {
	switch dist {
	case v1alpha1.DistributionVanilla:
		kindConfig := &v1alpha4.Cluster{Name: clusterName}

		return kindprovisioner.CreateProvisioner(kindConfig, "")

	case v1alpha1.DistributionK3s:
		k3dConfig := &k3dv1alpha5.SimpleConfig{
			ObjectMeta: k3dtypes.ObjectMeta{Name: clusterName},
		}

		return k3dprovisioner.CreateProvisioner(k3dConfig, ""), nil

	case v1alpha1.DistributionTalos:
		talosConfig := &talosconfigmanager.Configs{Name: clusterName}

		return talosprovisioner.CreateProvisioner(
			talosConfig,
			"",
			v1alpha1.ProviderDocker,
			v1alpha1.OptionsTalos{},
			v1alpha1.OptionsHetzner{},
			false,
		)

	default:
		return nil, fmt.Errorf("unsupported distribution: %s", dist)
	}
}
