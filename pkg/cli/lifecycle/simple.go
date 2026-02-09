package lifecycle

import (
	"context"
	"errors"
	"fmt"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	configmanager "github.com/devantler-tech/ksail/v5/pkg/io/config-manager"
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

// Lifecycle errors.
var (
	// ErrClusterNameRequired indicates that a cluster name is required but was not provided.
	ErrClusterNameRequired = errors.New(
		"cluster name is required: use --name flag, create a ksail.yaml config, or set a kubeconfig context",
	)

	// ErrClusterNotFoundInDistributions indicates the cluster was not found in any distribution.
	ErrClusterNotFoundInDistributions = errors.New("cluster not found in any distribution")

	// ErrCreateNotSupported indicates create is not supported without specifying distribution.
	ErrCreateNotSupported = errors.New("create not supported without specifying distribution")

	// ErrUnknownDistribution indicates an unknown distribution was specified.
	ErrUnknownDistribution = errors.New("unknown distribution")
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

	resolvedPath, err := resolveKubeconfigPath(kubeconfigPath)
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
	clusterInfo, err := DetectClusterInfo(kubeconfigPath, "")
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
	clusterInfo := &ClusterInfo{
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
		return "", fmt.Errorf("%w: %s", ErrUnknownDistribution, clusterInfo.Distribution)
	}
}

// CreateMinimalProvisioner creates a minimal provisioner for lifecycle operations.
// These provisioners only need enough configuration to identify containers.
// It uses the detected ClusterInfo to create the appropriate provisioner
// with the correct provider configuration.
func CreateMinimalProvisioner(
	info *ClusterInfo,
) (clusterprovisioner.ClusterProvisioner, error) {
	return createProvisionerForDistribution(
		info.Distribution,
		info.ClusterName,
		info.KubeconfigPath,
		info.Provider,
	)
}

// CreateMinimalProvisionerForProvider creates provisioners for all distributions
// that support the given provider, and returns the first one that can operate on the cluster.
// This is used when we only have --name and --provider flags without distribution info.
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

// createDockerMultiProvisioner creates a provisioner that tries multiple distributions.
func createDockerMultiProvisioner(
	clusterName string,
) (clusterprovisioner.ClusterProvisioner, error) {
	return &multiDistributionProvisioner{
		clusterName: clusterName,
	}, nil
}

// multiDistributionProvisioner wraps multiple provisioners and routes operations
// to the appropriate one based on which cluster exists.
type multiDistributionProvisioner struct {
	clusterName string
}

// clusterOperation is a function that operates on a provisioner with a cluster name.
type clusterOperation func(clusterprovisioner.ClusterProvisioner, string) error

// getSupportedDistributions returns all supported distributions in priority order.
func getSupportedDistributions() []v1alpha1.Distribution {
	return []v1alpha1.Distribution{
		v1alpha1.DistributionVanilla,
		v1alpha1.DistributionK3s,
		v1alpha1.DistributionTalos,
	}
}

// Start starts the cluster by trying each distribution's provisioner.
func (m *multiDistributionProvisioner) Start(ctx context.Context, name string) error {
	return m.delegateToExisting(ctx, name, "start",
		func(p clusterprovisioner.ClusterProvisioner, n string) error { return p.Start(ctx, n) },
	)
}

// Stop stops the cluster by trying each distribution's provisioner.
func (m *multiDistributionProvisioner) Stop(ctx context.Context, name string) error {
	return m.delegateToExisting(ctx, name, "stop",
		func(p clusterprovisioner.ClusterProvisioner, n string) error { return p.Stop(ctx, n) },
	)
}

// Delete deletes the cluster by trying each distribution's provisioner.
func (m *multiDistributionProvisioner) Delete(ctx context.Context, name string) error {
	return m.delegateToExisting(ctx, name, "delete",
		func(p clusterprovisioner.ClusterProvisioner, n string) error { return p.Delete(ctx, n) },
	)
}

// Exists checks if the cluster exists in any distribution.
func (m *multiDistributionProvisioner) Exists(ctx context.Context, name string) (bool, error) {
	found := false
	err := m.forExistingCluster(
		ctx,
		name,
		func(_ clusterprovisioner.ClusterProvisioner, _ string) error {
			found = true

			return nil
		},
	)

	// forExistingCluster returns ErrClusterNotFoundInDistributions if not found,
	// which is expected - we just return false in that case
	if err != nil && !errors.Is(err, ErrClusterNotFoundInDistributions) {
		return false, err
	}

	return found, nil
}

// List lists all clusters across all distributions.
func (m *multiDistributionProvisioner) List(ctx context.Context) ([]string, error) {
	var allClusters []string

	for _, dist := range getSupportedDistributions() {
		provisioner, err := createProvisionerForDistribution(dist, m.clusterName, "", "")
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
	return ErrCreateNotSupported
}

// delegateToExisting finds the existing cluster and delegates the operation,
// wrapping any error with a descriptive verb.
func (m *multiDistributionProvisioner) delegateToExisting(
	ctx context.Context,
	name string,
	verb string,
	operation clusterOperation,
) error {
	return m.forExistingCluster(
		ctx,
		name,
		func(p clusterprovisioner.ClusterProvisioner, n string) error {
			err := operation(p, n)
			if err != nil {
				return fmt.Errorf("failed to %s cluster: %w", verb, err)
			}

			return nil
		},
	)
}

// forExistingCluster finds an existing cluster across all distributions and applies an operation.
// Returns ErrClusterNotFoundInDistributions if the cluster doesn't exist in any distribution.
func (m *multiDistributionProvisioner) forExistingCluster(
	ctx context.Context,
	name string,
	operation clusterOperation,
) error {
	clusterName := name
	if clusterName == "" {
		clusterName = m.clusterName
	}

	for _, dist := range getSupportedDistributions() {
		provisioner, err := createProvisionerForDistribution(dist, clusterName, "", "")
		if err != nil {
			continue
		}

		exists, err := provisioner.Exists(ctx, clusterName)
		if err != nil {
			continue
		}

		if exists {
			return operation(provisioner, clusterName)
		}
	}

	return fmt.Errorf("%w: %s", ErrClusterNotFoundInDistributions, clusterName)
}

// createProvisionerForDistribution creates a provisioner for a specific distribution.
// The kubeconfigPath and providerType parameters allow callers that have
// richer context (e.g., ClusterInfo) to pass it through; when empty, defaults
// are used ("" and ProviderDocker respectively).
func createProvisionerForDistribution(
	dist v1alpha1.Distribution,
	clusterName string,
	kubeconfigPath string,
	providerType v1alpha1.Provider,
) (clusterprovisioner.ClusterProvisioner, error) {
	if providerType == "" {
		providerType = v1alpha1.ProviderDocker
	}

	switch dist {
	case v1alpha1.DistributionVanilla:
		kindConfig := &v1alpha4.Cluster{Name: clusterName}

		provisioner, err := kindprovisioner.CreateProvisioner(kindConfig, "")
		if err != nil {
			return nil, fmt.Errorf("failed to create Kind provisioner: %w", err)
		}

		return provisioner, nil

	case v1alpha1.DistributionK3s:
		k3dConfig := &k3dv1alpha5.SimpleConfig{
			ObjectMeta: k3dtypes.ObjectMeta{Name: clusterName},
		}

		return k3dprovisioner.CreateProvisioner(k3dConfig, ""), nil

	case v1alpha1.DistributionTalos:
		talosConfig := &talosconfigmanager.Configs{Name: clusterName}

		provisioner, err := talosprovisioner.CreateProvisioner(
			talosConfig,
			kubeconfigPath,
			providerType,
			v1alpha1.OptionsTalos{},
			v1alpha1.OptionsHetzner{},
			false,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to create Talos provisioner: %w", err)
		}

		return provisioner, nil

	default:
		return nil, fmt.Errorf("%w: %s", clusterprovisioner.ErrUnsupportedDistribution, dist)
	}
}
