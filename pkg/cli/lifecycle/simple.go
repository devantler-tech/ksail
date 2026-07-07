package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/flags"
	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	configmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager"
	ksailconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/ksail"
	talosconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/talos"
	"github.com/devantler-tech/ksail/v7/pkg/k8s"
	"github.com/devantler-tech/ksail/v7/pkg/notify"
	clusterdetector "github.com/devantler-tech/ksail/v7/pkg/svc/detector/cluster"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clustererr"
	talosprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/talos"
	"github.com/spf13/cobra"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// Lifecycle errors.
var (
	// ErrClusterNameRequired indicates that a cluster name is required but was not provided.
	ErrClusterNameRequired = errors.New(
		"cluster name is required: use --name flag, create a ksail.yaml config, or set a kubeconfig context",
	)
)

const (
	namespaceKsailPrefix    = "ksail-"
	namespaceK3kPrefix      = "k3k-"
	namespaceVClusterPrefix = "vcluster-"
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
	// Guard, when non-nil, runs after cluster resolution and before the provisioner is created. It
	// lets a caller refuse the action for a resolved cluster — e.g. an unmanaged, ksail-unprovisioned
	// cluster — with a clear error. A nil Guard is a no-op.
	Guard func(ctx context.Context, resolved *ResolvedClusterInfo) error
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

	BindNameAndProviderFlags(cmd, &nameFlag, &providerFlag)

	return cmd
}

// BindNameAndProviderFlags registers the shared --name and --provider flags used
// by cluster-targeting commands (start, stop, info, diagnose).
func BindNameAndProviderFlags(
	cmd *cobra.Command,
	nameFlag *string,
	providerFlag *v1alpha1.Provider,
) {
	cmd.Flags().StringVarP(nameFlag, "name", "n", "", "Name of the cluster to target")
	cmd.Flags().VarP(
		providerFlag,
		"provider",
		"p",
		fmt.Sprintf("Provider to use (%s)", strings.Join(providerFlag.ValidValues(), ", ")),
	)
}

// ResolvedClusterInfo contains the resolved cluster name, provider, and kubeconfig path.
type ResolvedClusterInfo struct {
	ClusterName    string
	Provider       v1alpha1.Provider
	KubeconfigPath string
	OmniOpts       v1alpha1.OptionsOmni
	KubernetesOpts v1alpha1.OptionsKubernetes
	// AWSRegion is the resolved AWS region for read-only EKS status lookups.
	// Empty defers region resolution to eksctl (AWS_REGION env / active profile).
	AWSRegion string
}

// awsRegionEnvVarDefault is the fallback environment variable name for the AWS
// region when spec.provider.aws.regionEnvVar is unset (mirrors the OptionsAWS
// default so the info path resolves the region the same way create does).
const awsRegionEnvVarDefault = "AWS_REGION"

// resolveAWSRegion determines the AWS region for read-only EKS status lookups,
// honoring the documented precedence from OptionsAWS: the environment variable
// named by RegionEnvVar (default AWS_REGION) overrides the region declared in
// eks.yaml. An empty result tells eksctl to fall back to its own resolution, so
// this never regresses the previous always-empty behavior.
func resolveAWSRegion(
	awsOpts v1alpha1.OptionsAWS,
	distCfg *clusterprovisioner.DistributionConfig,
) string {
	envVar := awsOpts.RegionEnvVar
	if envVar == "" {
		envVar = awsRegionEnvVarDefault
	}

	if region := os.Getenv(envVar); region != "" {
		return region
	}

	if distCfg != nil && distCfg.EKS != nil {
		return distCfg.EKS.Region
	}

	return ""
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

	// Always load config to fill missing fields and extract Omni/Kubernetes options.
	// Even when --name is provided, we still need Omni endpoint from config.
	var (
		omniOpts       v1alpha1.OptionsOmni
		kubernetesOpts v1alpha1.OptionsKubernetes
		awsRegion      string
	)

	resolveFromConfig(
		cmd, &clusterName, &provider, &kubeconfigPath, &omniOpts, &kubernetesOpts, &awsRegion,
	)

	// Fall back to kubeconfig context detection
	if clusterName == "" {
		resolveFromKubecontext(commandContext(cmd), &clusterName, &provider, kubeconfigPath)
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
		KubernetesOpts: kubernetesOpts,
		AWSRegion:      awsRegion,
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

// resolveFromConfig fills missing cluster info and provider options from the ksail.yaml config.
// When cmd is non-nil, the --config persistent flag is honored.
// Fields that already have values (from flags) are not overwritten.
func resolveFromConfig(
	cmd *cobra.Command,
	clusterName *string,
	provider *v1alpha1.Provider,
	kubeconfigPath *string,
	omniOpts *v1alpha1.OptionsOmni,
	kubernetesOpts *v1alpha1.OptionsKubernetes,
	awsRegion *string,
) {
	cfg, distCfg := loadConfig(cmd)
	if cfg == nil {
		return
	}

	if *clusterName == "" && cfg.Name != "" {
		if v1alpha1.ValidateClusterName(cfg.Name) == nil {
			*clusterName = cfg.Name
		}
	}

	if *clusterName == "" {
		*clusterName = ClusterNameFromDistributionConfig(distCfg)
	}

	if *provider == "" && cfg.Spec.Cluster.Provider != "" {
		*provider = cfg.Spec.Cluster.Provider
	}

	if *kubeconfigPath == "" && cfg.Spec.Cluster.Connection.Kubeconfig != "" {
		*kubeconfigPath = cfg.Spec.Cluster.Connection.Kubeconfig
	}

	*omniOpts = cfg.Spec.Provider.Omni
	*kubernetesOpts = cfg.Spec.Provider.Kubernetes
	*awsRegion = resolveAWSRegion(cfg.Spec.Provider.AWS, distCfg)
}

// commandContext returns cmd's context, falling back to context.Background()
// when cmd is nil (ResolveClusterInfo tolerates a nil command).
func commandContext(cmd *cobra.Command) context.Context {
	if cmd == nil {
		return context.Background()
	}

	return cmd.Context()
}

// resolveFromKubecontext fills missing cluster info from the current kubeconfig context.
func resolveFromKubecontext(
	ctx context.Context,
	clusterName *string,
	provider *v1alpha1.Provider,
	kubeconfigPath string,
) {
	clusterInfo, err := clusterdetector.DetectInfo(ctx, kubeconfigPath, "")
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

	if config.Guard != nil {
		err = config.Guard(cmd.Context(), resolved)
		if err != nil {
			return err
		}
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

	return provisionAndAct(cmd, config, resolved)
}

// provisionAndAct creates a minimal provisioner for the resolved cluster, runs the
// configured lifecycle action, and emits the success message on completion.
func provisionAndAct(
	cmd *cobra.Command,
	config SimpleLifecycleConfig,
	resolved *ResolvedClusterInfo,
) error {
	// Create cluster info for provisioner creation
	clusterInfo := &clusterdetector.Info{
		ClusterName:    resolved.ClusterName,
		Provider:       resolved.Provider,
		KubeconfigPath: resolved.KubeconfigPath,
	}

	provisioner, err := CreateMinimalProvisionerForProvider(
		clusterInfo,
		resolved.OmniOpts,
		resolved.KubernetesOpts,
		false,
	)
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
	kubernetesOpts v1alpha1.OptionsKubernetes,
	deleteStorage bool,
) (clusterprovisioner.Provisioner, error) {
	switch info.Provider {
	case v1alpha1.ProviderDocker, "":
		// Docker provider supports all Docker-based distributions -
		// create a multi-provisioner that tries each distribution in order
		return clusterprovisioner.NewMultiProvisioner(info.ClusterName), nil

	case v1alpha1.ProviderKubernetes:
		// Kubernetes provider runs clusters as pods in a host cluster.
		// Delete by removing the ksail-<name> namespace (cascading delete).
		// Pass info.KubeconfigPath as the nested cluster's kubeconfig for context cleanup.
		return newKubernetesCleanupProvisioner(
			info.ClusterName,
			kubernetesOpts,
			info.KubeconfigPath,
		)

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

		provisioner.WithDeleteStorage(deleteStorage)

		return provisioner, nil

	case v1alpha1.ProviderAWS, v1alpha1.ProviderGCP, v1alpha1.ProviderAzure:
		// AWS, GCP, and Azure only support their managed distributions
		// (EKS/GKE/AKS), which are not docker-based and cannot be produced by
		// the minimal multi-provisioner path; their lifecycle goes through the factory.
		return nil, fmt.Errorf(
			"%w: %s provider is only supported via its managed distribution",
			clusterprovisioner.ErrUnsupportedProvider,
			info.Provider,
		)

	default:
		return nil, fmt.Errorf(
			"%w: %s",
			clusterprovisioner.ErrUnsupportedProvider,
			info.Provider,
		)
	}
}

// kubernetesCleanupProvisioner deletes nested clusters on a Kubernetes provider
// by removing the ksail-<name> namespace (cascading delete removes all resources).
type kubernetesCleanupProvisioner struct {
	clusterName          string
	clientset            kubernetes.Interface
	kubeconfigPath       string
	nestedKubeconfigPath string
}

func newKubernetesCleanupProvisioner(
	clusterName string,
	opts v1alpha1.OptionsKubernetes,
	nestedKubeconfigPath string,
) (*kubernetesCleanupProvisioner, error) {
	kubeconfig := resolveKubernetesOption(opts.Kubeconfig, opts.KubeconfigEnvVar)
	if kubeconfig == "" {
		kubeconfig = k8s.DefaultKubeconfigPath()
	}

	kubeconfig, err := fsutil.ExpandHomePath(kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("expand kubeconfig path: %w", err)
	}

	kubeconfig, err = fsutil.EvalCanonicalPath(kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("canonicalize kubeconfig path: %w", err)
	}

	contextName := resolveKubernetesOption(opts.Context, opts.ContextEnvVar)

	restConfig, err := k8s.BuildRESTConfig(kubeconfig, contextName)
	if err != nil {
		return nil, fmt.Errorf("build host REST config: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("create host clientset: %w", err)
	}

	return &kubernetesCleanupProvisioner{
		clusterName:          clusterName,
		clientset:            clientset,
		kubeconfigPath:       kubeconfig,
		nestedKubeconfigPath: nestedKubeconfigPath,
	}, nil
}

func (p *kubernetesCleanupProvisioner) Create(_ context.Context, _ string) error {
	return fmt.Errorf("create: %w", clustererr.ErrOperationNotSupported)
}

func (p *kubernetesCleanupProvisioner) Delete(ctx context.Context, _ string) error {
	// Try all known namespace prefixes used by nested cluster provisioners:
	// - "ksail-" for DinD-based Kind-on-Kubernetes
	// - "k3k-" for k3k-based K3s-on-Kubernetes
	// - "vcluster-" for vCluster-on-Kubernetes (Helm driver)
	for _, prefix := range []string{namespaceKsailPrefix, namespaceK3kPrefix, namespaceVClusterPrefix} {
		namespaceName := prefix + p.clusterName
		err := p.verifyAndDeleteNamespace(ctx, namespaceName)
		//nolint:wsl
		if err != nil {
			return err
		}
	}

	// Clean up nested cluster kubeconfig entries using the nested cluster's kubeconfig
	// (spec.cluster.connection.kubeconfig), not the host kubeconfig.
	cleanupPath := p.nestedKubeconfigPath
	if cleanupPath == "" {
		cleanupPath = p.kubeconfigPath
	}

	for _, prefix := range []string{"kind-", namespaceK3kPrefix, namespaceVClusterPrefix, "admin@", "kwok-"} {
		contextName := prefix + p.clusterName
		_ = k8s.CleanupKubeconfig(cleanupPath, contextName, contextName, contextName, io.Discard)
	}

	return nil
}

func (p *kubernetesCleanupProvisioner) Start(_ context.Context, _ string) error {
	return fmt.Errorf("start: %w", clustererr.ErrOperationNotSupported)
}

func (p *kubernetesCleanupProvisioner) Stop(_ context.Context, _ string) error {
	return fmt.Errorf("stop: %w", clustererr.ErrOperationNotSupported)
}

func (p *kubernetesCleanupProvisioner) Exists(ctx context.Context, _ string) (bool, error) {
	// Check all known namespace prefixes used by nested cluster provisioners
	for _, prefix := range []string{namespaceKsailPrefix, namespaceK3kPrefix, namespaceVClusterPrefix} {
		namespaceName := prefix + p.clusterName

		_, err := p.clientset.CoreV1().Namespaces().Get(ctx, namespaceName, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			continue
		}

		if err != nil {
			return false, fmt.Errorf("check namespace %s: %w", namespaceName, err)
		}

		return true, nil
	}

	return false, nil
}

func (p *kubernetesCleanupProvisioner) List(ctx context.Context) ([]string, error) {
	nsList, err := p.clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{
		LabelSelector: "ksail.io/managed-by=ksail",
	})
	if err != nil {
		return nil, fmt.Errorf("list ksail namespaces: %w", err)
	}

	var names []string

	for _, ns := range nsList.Items {
		name := ns.Name
		// Strip known namespace prefixes to get the cluster name
		for _, prefix := range []string{namespaceKsailPrefix, namespaceK3kPrefix, namespaceVClusterPrefix} {
			if after, ok := strings.CutPrefix(name, prefix); ok {
				name = after

				break
			}
		}

		names = append(names, name)
	}

	return names, nil
}

// verifyAndDeleteNamespace checks if a namespace is KSail-managed and deletes it if so.
// It returns nil if the namespace was successfully deleted or does not exist.
// It returns an error only if deletion fails for a KSail-managed namespace.
func (p *kubernetesCleanupProvisioner) verifyAndDeleteNamespace(
	ctx context.Context,
	namespaceName string,
) error {
	namespace, err := p.clientset.CoreV1().Namespaces().Get(ctx, namespaceName, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			// Namespace doesn't exist, nothing to do
			return nil
		}
		// On other errors, skip this prefix
		return nil
	}

	// Verify that this namespace is KSail-managed before deleting
	if namespace.Labels != nil &&
		namespace.Labels["ksail.io/managed-by"] == "ksail" &&
		namespace.Labels["ksail.io/cluster"] == p.clusterName {
		// This is a KSail-managed namespace, safe to delete
		err := p.clientset.CoreV1().Namespaces().Delete(ctx, namespaceName, metav1.DeleteOptions{})
		if err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("delete namespace %s: %w", namespaceName, err)
		}
	}

	return nil
}

// resolveKubernetesOption resolves a value from an environment variable (preferred) or direct config value.
func resolveKubernetesOption(value, envVar string) string {
	if envVar != "" {
		if envValue := os.Getenv(envVar); envValue != "" {
			return envValue
		}
	}

	return value
}
