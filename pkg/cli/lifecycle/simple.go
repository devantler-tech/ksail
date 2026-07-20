package lifecycle

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
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
	"github.com/devantler-tech/ksail/v7/pkg/svc/credentials"
	clusterdetector "github.com/devantler-tech/ksail/v7/pkg/svc/detector/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/svc/eksidentity"
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
	// ErrAWSTargetConfigMismatch indicates that a resolved EKS target would inherit the region
	// from a local EKS config describing a different cluster.
	ErrAWSTargetConfigMismatch = errors.New("resolved EKS target does not match local config")
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
	ClusterName       string
	ConfigClusterName string
	// ConfigSource reports that a real ksail.yaml was loaded. Empty AWS option names from a loaded
	// config deliberately select canonical defaults and must not be overwritten by persisted aliases.
	ConfigSource bool
	// EKSConfigSource reports that ConfigClusterName came from an actual loaded eks.yaml, rather
	// than another distribution config or a synthetic fallback.
	EKSConfigSource bool
	Provider        v1alpha1.Provider
	KubeconfigPath  string
	OmniOpts        v1alpha1.OptionsOmni
	KubernetesOpts  v1alpha1.OptionsKubernetes
	// AWSRegion is the resolved AWS region for EKS operations.
	// Empty defers region resolution to eksctl (AWS_REGION env / active profile).
	AWSRegion string
	// AWSRegionFromConfig reports that AWSRegion came from the loaded EKS config rather than the
	// configured region environment variable. Mutating standalone commands use this provenance to
	// avoid applying one config's region to a different resolved target.
	AWSRegionFromConfig bool
	// AWSOpts retains the credential environment-variable mappings from the loaded cluster config.
	AWSOpts v1alpha1.OptionsAWS
	// AWSResolution pins the concrete credentials selected by the EKS ownership guard for the
	// provisioner that performs the authorized mutation. It is never persisted.
	AWSResolution *credentials.AWSResolution
	// AWSOwnershipVerifier rechecks the persisted/live EKS incarnation immediately before mutation.
	AWSOwnershipVerifier AWSOwnershipVerifier
}

// AWSOwnershipVerifier is a read-only immutable EKS identity check captured by the initial guard.
type AWSOwnershipVerifier = eksidentity.Verifier

// awsRegionEnvVarDefault is the fallback environment variable name for the AWS
// region when spec.provider.aws.regionEnvVar is unset (mirrors the OptionsAWS
// default so the info path resolves the region the same way create does).
const awsRegionEnvVarDefault = "AWS_REGION"

// ResolveAWSRegion determines the AWS region for EKS operations,
// honoring the documented precedence from OptionsAWS: the environment variable
// named by RegionEnvVar (default AWS_REGION) overrides the region declared in
// eks.yaml. An empty result tells eksctl to fall back to its own resolution, so
// this never regresses the previous always-empty behavior.
func ResolveAWSRegion(
	awsOpts v1alpha1.OptionsAWS,
	distCfg *clusterprovisioner.DistributionConfig,
) string {
	region, _ := resolveAWSRegion(awsOpts, distCfg)

	return region
}

// resolveAWSRegion returns the resolved region together with whether it came from eks.yaml.
func resolveAWSRegion(
	awsOpts v1alpha1.OptionsAWS,
	distCfg *clusterprovisioner.DistributionConfig,
) (string, bool) {
	envVar := awsOpts.RegionEnvVar
	if envVar == "" {
		envVar = awsRegionEnvVarDefault
	}

	if region := os.Getenv(envVar); region != "" {
		return region, false
	}

	// A declarative region is safe to reuse only when the same actual eks.yaml also named the
	// cluster it describes. ConfigManager can load a nameless file and synthesize a target name;
	// treating that file's region as target-bound could redirect a standalone mutation.
	if distCfg != nil && distCfg.EKS != nil && distCfg.EKS.NameFromConfig {
		return distCfg.EKS.Region, distCfg.EKS.Region != ""
	}

	return "", false
}

// ResolveClusterInfo resolves the cluster name, provider, and kubeconfig from flags, config, or kubeconfig.
// Priority for cluster name: flag > config > kubeconfig context.
// Priority for provider: flag > config > default (Docker).
// Priority for kubeconfig: flag > env (KUBECONFIG) > config > default (~/.kube/config).
//
// When cmd is non-nil, the --config persistent flag is honored for config loading.
//
// A config that is present but unreadable is tolerated: resolution falls back to flags and the
// kubeconfig context so read-only commands (cluster info, diagnose, connect) keep working while
// local project configuration is broken. Callers that are about to MUTATE a cluster must use
// ResolveClusterInfoStrict instead, so a malformed config can never silently drop credential
// aliases or region bindings ahead of a destructive operation.
func ResolveClusterInfo(
	cmd *cobra.Command,
	nameFlag string,
	providerFlag v1alpha1.Provider,
	kubeconfigFlag string,
) (*ResolvedClusterInfo, error) {
	return resolveClusterInfo(cmd, nameFlag, providerFlag, kubeconfigFlag, false)
}

// ResolveClusterInfoStrict resolves cluster info like ResolveClusterInfo but fails closed when a
// present config cannot be loaded. Mutating lifecycle commands use it so a malformed ksail.yaml or
// distribution config aborts before any cluster is changed, rather than proceeding on partially
// resolved identity.
func ResolveClusterInfoStrict(
	cmd *cobra.Command,
	nameFlag string,
	providerFlag v1alpha1.Provider,
	kubeconfigFlag string,
) (*ResolvedClusterInfo, error) {
	return resolveClusterInfo(cmd, nameFlag, providerFlag, kubeconfigFlag, true)
}

func resolveClusterInfo(
	cmd *cobra.Command,
	nameFlag string,
	providerFlag v1alpha1.Provider,
	kubeconfigFlag string,
	strict bool,
) (*ResolvedClusterInfo, error) {
	resolved := ResolvedClusterInfo{
		ClusterName:    nameFlag,
		Provider:       providerFlag,
		KubeconfigPath: kubeconfigFlag,
	}

	// Always load config to fill missing fields and extract provider options: even when --name is
	// provided, provider-specific settings are still needed.
	err := resolveFromConfig(cmd, &resolved)
	if err != nil {
		if strict {
			return nil, err
		}

		slog.Warn(
			"ignoring unreadable configuration; resolving from flags and kubeconfig instead",
			"error", err,
		)
	}

	// Fall back to kubeconfig context detection
	if resolved.ClusterName == "" {
		resolveFromKubecontext(
			commandContext(cmd),
			&resolved.ClusterName,
			&resolved.Provider,
			resolved.KubeconfigPath,
		)
	}

	if resolved.ClusterName == "" {
		return nil, ErrClusterNameRequired
	}

	if resolved.Provider == "" {
		resolved.Provider = v1alpha1.ProviderDocker
	}

	resolvedPath, err := clusterdetector.ResolveKubeconfigPath(resolved.KubeconfigPath)
	if err != nil {
		return nil, fmt.Errorf("resolve kubeconfig path: %w", err)
	}

	resolved.KubeconfigPath = resolvedPath

	return &resolved, nil
}

// loadConfig loads the ksail.yaml config, honoring the --config flag when cmd is non-nil. A
// genuinely absent default config is a supported standalone-command case; a present, explicit, or
// discovered config that cannot be loaded is returned as an error so destructive callers fail
// closed instead of falling back to ambient provider credentials.
func loadConfig(
	cmd *cobra.Command,
) (*v1alpha1.Cluster, *clusterprovisioner.DistributionConfig, error) {
	var configFile string

	if cmd != nil {
		cfgPath, err := flags.GetConfigPath(cmd)
		if err != nil {
			return nil, nil, fmt.Errorf("resolve config path: %w", err)
		}

		configFile = cfgPath
	}

	cfgManager := ksailconfigmanager.NewConfigManager(nil, configFile)

	cfg, err := cfgManager.Load(configmanager.LoadOptions{Silent: true, SkipValidation: true})
	if err != nil {
		return nil, nil, fmt.Errorf("load cluster config: %w", err)
	}

	if cfg == nil || !cfgManager.IsConfigFileFound() {
		return nil, nil, nil
	}

	return cfg, cfgManager.DistributionConfig, nil
}

// resolveFromConfig fills missing cluster info and provider options from the ksail.yaml config.
// When cmd is non-nil, the --config persistent flag is honored.
// Fields that already have values (from flags) are not overwritten.
func resolveFromConfig(
	cmd *cobra.Command,
	resolved *ResolvedClusterInfo,
) error {
	cfg, distCfg, err := loadConfig(cmd)
	if err != nil {
		return err
	}

	if cfg == nil {
		return nil
	}

	resolved.ConfigSource = true
	resolveClusterIdentityFromConfig(resolved, cfg, distCfg)

	resolved.OmniOpts = cfg.Spec.Provider.Omni
	resolved.KubernetesOpts = cfg.Spec.Provider.Kubernetes
	resolved.AWSOpts = cfg.Spec.Provider.AWS
	resolved.AWSRegion, resolved.AWSRegionFromConfig = resolveAWSRegion(
		cfg.Spec.Provider.AWS,
		distCfg,
	)

	return nil
}

func resolveClusterIdentityFromConfig(
	resolved *ResolvedClusterInfo,
	cfg *v1alpha1.Cluster,
	distCfg *clusterprovisioner.DistributionConfig,
) {
	resolved.ConfigClusterName = resolveConfigClusterName(cfg, distCfg)
	resolved.EKSConfigSource = hasLoadedEKSConfigSource(cfg, distCfg, resolved.ConfigClusterName)
	resolved.ClusterName = resolveClusterNameFromConfig(
		resolved.ClusterName,
		resolved.ConfigClusterName,
		resolved.EKSConfigSource,
		cfg,
		distCfg,
	)

	if resolved.Provider == "" && cfg.Spec.Cluster.Provider != "" {
		resolved.Provider = cfg.Spec.Cluster.Provider
	}

	if resolved.KubeconfigPath == "" && cfg.Spec.Cluster.Connection.Kubeconfig != "" {
		resolved.KubeconfigPath = cfg.Spec.Cluster.Connection.Kubeconfig
	}
}

func hasLoadedEKSConfigSource(
	cfg *v1alpha1.Cluster,
	distCfg *clusterprovisioner.DistributionConfig,
	configClusterName string,
) bool {
	return cfg.Spec.Cluster.Distribution == v1alpha1.DistributionEKS &&
		distCfg != nil && distCfg.EKS != nil &&
		distCfg.EKS.NameFromConfig &&
		strings.TrimSpace(distCfg.EKS.ConfigPath) != "" &&
		strings.TrimSpace(configClusterName) != ""
}

func resolveClusterNameFromConfig(
	currentName, configClusterName string,
	eksConfigSource bool,
	cfg *v1alpha1.Cluster,
	distCfg *clusterprovisioner.DistributionConfig,
) string {
	if currentName != "" {
		return currentName
	}

	// eksctl creates and deletes the name encoded in the actual eks.yaml source. ConfigManager may
	// synthesize "eks-default" when the file is absent, so only grant this priority when ConfigPath
	// proved the source exists.
	if eksConfigSource {
		return configClusterName
	}

	if cfg.Name != "" && v1alpha1.ValidateClusterName(cfg.Name) == nil {
		return cfg.Name
	}

	return ClusterNameFromDistributionConfig(distCfg)
}

// resolveConfigClusterName returns the distribution config's name when available because it is the
// source of any fallback AWS region. The top-level metadata name is the safe fallback.
func resolveConfigClusterName(
	cfg *v1alpha1.Cluster,
	distCfg *clusterprovisioner.DistributionConfig,
) string {
	if cfg.Spec.Cluster.Distribution == v1alpha1.DistributionEKS &&
		distCfg != nil && distCfg.EKS != nil {
		if strings.TrimSpace(distCfg.EKS.ConfigPath) == "" || !distCfg.EKS.NameFromConfig {
			return ""
		}

		return strings.TrimSpace(distCfg.EKS.Name)
	}

	if name := ClusterNameFromDistributionConfig(distCfg); name != "" {
		return name
	}

	if v1alpha1.ValidateClusterName(cfg.Name) == nil {
		return cfg.Name
	}

	return ""
}

// ValidateStandaloneAWSTarget rejects the ambiguous destructive case where the resolved target
// differs from the cluster described by the local EKS config while the only resolved region came
// from that config. Requiring the configured region environment variable makes the target pair
// deliberate and prevents delete/start/stop from mutating a same-named cluster in the wrong region.
func ValidateStandaloneAWSTarget(resolved *ResolvedClusterInfo) error {
	if resolved == nil || resolved.Provider != v1alpha1.ProviderAWS ||
		!resolved.AWSRegionFromConfig ||
		resolved.ConfigClusterName == "" || resolved.ConfigClusterName == resolved.ClusterName {
		return nil
	}

	regionEnvVar := resolved.AWSOpts.RegionEnvVar
	if regionEnvVar == "" {
		regionEnvVar = awsRegionEnvVarDefault
	}

	return fmt.Errorf(
		"resolved EKS target %q does not match EKS config cluster %q: "+
			"refusing to reuse region %q from that config; set %s to select the target explicitly: %w",
		resolved.ClusterName,
		resolved.ConfigClusterName,
		resolved.AWSRegion,
		regionEnvVar,
		ErrAWSTargetConfigMismatch,
	)
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

	// Resolve cluster info from flags, config, or kubeconfig. These commands mutate the cluster, so a
	// present-but-unreadable config must abort before any change.
	// Empty kubeconfig flag - simple lifecycle commands don't need kubeconfig cleanup
	resolved, err := ResolveClusterInfoStrict(cmd, nameFlag, providerFlag, "")
	if err != nil {
		return err
	}

	err = ValidateStandaloneAWSTarget(resolved)
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
		cmd.Context(),
		clusterInfo,
		MinimalProvisionerOptions{
			OmniOpts:             resolved.OmniOpts,
			KubernetesOpts:       resolved.KubernetesOpts,
			AWSOpts:              resolved.AWSOpts,
			AWSRegion:            resolved.AWSRegion,
			AWSResolution:        resolved.AWSResolution,
			AWSOwnershipVerifier: resolved.AWSOwnershipVerifier,
		},
	)
	if err != nil {
		return fmt.Errorf("failed to create provisioner: %w", err)
	}

	err = VerifyAWSOwnershipBeforeMutation(cmd.Context(), resolved.AWSOwnershipVerifier)
	if err != nil {
		return err
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

// MinimalProvisionerOptions contains the provider-specific context needed to
// construct a standalone lifecycle provisioner from only a resolved target.
type MinimalProvisionerOptions struct {
	OmniOpts             v1alpha1.OptionsOmni
	KubernetesOpts       v1alpha1.OptionsKubernetes
	AWSOpts              v1alpha1.OptionsAWS
	AWSRegion            string
	AWSResolution        *credentials.AWSResolution
	AWSOwnershipVerifier AWSOwnershipVerifier
	DeleteStorage        bool
}

// VerifyAWSOwnershipBeforeMutation invokes the EKS identity boundary when present. Non-EKS
// lifecycle operations carry a nil verifier and remain unchanged.
func VerifyAWSOwnershipBeforeMutation(
	ctx context.Context,
	verifier AWSOwnershipVerifier,
) error {
	err := eksidentity.VerifyBeforeMutation(ctx, verifier)
	if err != nil {
		return fmt.Errorf("verify AWS ownership before lifecycle mutation: %w", err)
	}

	return nil
}

// CreateMinimalProvisionerForProvider creates provisioners for all distributions
// that support the given provider, and returns the first one that can operate on the cluster.
// This is used when we only have --name and --provider flags without distribution info.
func CreateMinimalProvisionerForProvider(
	ctx context.Context,
	info *clusterdetector.Info,
	options MinimalProvisionerOptions,
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
			options.KubernetesOpts,
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
			options.OmniOpts,
			false,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to create talos provisioner: %w", err)
		}

		provisioner.WithDeleteStorage(options.DeleteStorage)

		return provisioner, nil

	case v1alpha1.ProviderAWS:
		return createMinimalEKSProvisioner(ctx, info, options)

	case v1alpha1.ProviderGCP, v1alpha1.ProviderAzure:
		// GCP and Azure only support their managed distributions (GKE/AKS),
		// which are not yet wired into this standalone lifecycle path.
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

// createMinimalEKSProvisioner builds the name-and-region-only EKS configuration
// needed by standalone delete/start/stop commands, while reusing the default
// factory's credential-isolated eksctl and AWS provider wiring. ConfigPath is
// deliberately empty: an explicit/resolved cluster name must never be replaced
// by a possibly unrelated local eks.yaml during a destructive delete.
func createMinimalEKSProvisioner(
	ctx context.Context,
	info *clusterdetector.Info,
	options MinimalProvisionerOptions,
) (clusterprovisioner.Provisioner, error) {
	clusterCfg := &v1alpha1.Cluster{}
	clusterCfg.Name = info.ClusterName
	clusterCfg.Spec.Cluster.Distribution = v1alpha1.DistributionEKS
	clusterCfg.Spec.Cluster.Provider = v1alpha1.ProviderAWS
	clusterCfg.Spec.Provider.AWS = options.AWSOpts

	factory := clusterprovisioner.DefaultFactory{
		AWSResolution:        options.AWSResolution,
		AWSOwnershipVerifier: options.AWSOwnershipVerifier,
		DistributionConfig: &clusterprovisioner.DistributionConfig{
			EKS: &clusterprovisioner.EKSConfig{
				Name:           info.ClusterName,
				Region:         options.AWSRegion,
				KubeconfigPath: info.KubeconfigPath,
			},
		},
	}

	provisioner, _, err := factory.Create(ctx, clusterCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create EKS provisioner: %w", err)
	}

	return provisioner, nil
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
