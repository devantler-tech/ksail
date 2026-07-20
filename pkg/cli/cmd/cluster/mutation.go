package cluster

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/clusterflags"
	kubeconfigutil "github.com/devantler-tech/ksail/v7/pkg/cli/kubeconfig"
	"github.com/devantler-tech/ksail/v7/pkg/cli/lifecycle"
	"github.com/devantler-tech/ksail/v7/pkg/cli/setup/localregistry"
	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	ksailconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/ksail"
	"github.com/devantler-tech/ksail/v7/pkg/svc/credentials"
	"github.com/devantler-tech/ksail/v7/pkg/svc/eksidentity"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	"github.com/spf13/cobra"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"
)

var (
	errEKSConfigurationUnavailable  = errors.New("EKS configuration is unavailable")
	errEKSClusterNameRequired       = errors.New("cluster name is required")
	errEKSMutationConfigNameNeeded  = errors.New("EKS config metadata.name is required")
	errEKSMutationConfigNameInvalid = errors.New("EKS config metadata.name is invalid")
	errEKSNameOverrideMismatch      = errors.New("cannot override EKS config cluster name")
	errNoMatchingEKSContext         = errors.New("no kubeconfig context matches EKS cluster")
	errAmbiguousEKSContext          = errors.New("multiple kubeconfig contexts match EKS cluster")
	errExplicitEKSContextUnobserved = errors.New(
		"explicit EKS context was not observed after creation",
	)
)

const explicitEKSContextHint = "set spec.cluster.connection.context explicitly"

const eksKubeconfigDirMode = 0o700

// defaultClusterMutationFieldSelectors returns the full set of field selectors
// used by commands that modify cluster state (create, update).
// This centralizes the selector list to avoid duplication between commands.
func defaultClusterMutationFieldSelectors() []ksailconfigmanager.FieldSelector[v1alpha1.Cluster] {
	selectors := ksailconfigmanager.DefaultClusterFieldSelectors()

	return append(
		selectors,
		// create/update/init expose -p=--provider (matching their lifecycle
		// siblings); shared read-only consumers of DefaultProviderFieldSelector
		// (e.g. workload images) keep the long flag only.
		ksailconfigmanager.WithProviderShorthand(ksailconfigmanager.DefaultProviderFieldSelector()),
		ksailconfigmanager.DefaultCNIFieldSelector(),
		ksailconfigmanager.DefaultMetricsServerFieldSelector(),
		ksailconfigmanager.DefaultLoadBalancerFieldSelector(),
		ksailconfigmanager.DefaultCertManagerFieldSelector(),
		ksailconfigmanager.DefaultPolicyEngineFieldSelector(),
		ksailconfigmanager.DefaultCSIFieldSelector(),
		ksailconfigmanager.DefaultCDIFieldSelector(),
		ksailconfigmanager.DefaultImportImagesFieldSelector(),
		ksailconfigmanager.KubernetesVersionFieldSelector(),
		ksailconfigmanager.DistributionVersionFieldSelector(),
		ksailconfigmanager.DrainTimeoutFieldSelector(),
		ksailconfigmanager.ControlPlanesFieldSelector(),
		ksailconfigmanager.WorkersFieldSelector(),
		ksailconfigmanager.NodeAutoscalingFieldSelector(), //nolint:staticcheck // backward compat
		ksailconfigmanager.NodeAutoscalerEnabledFieldSelector(),
		ksailconfigmanager.OIDCIssuerURLFieldSelector(),
		ksailconfigmanager.OIDCClientIDFieldSelector(),
		ksailconfigmanager.OIDCUsernameClaimFieldSelector(),
		ksailconfigmanager.OIDCUsernamePrefixFieldSelector(),
		ksailconfigmanager.OIDCGroupsClaimFieldSelector(),
		ksailconfigmanager.OIDCGroupsPrefixFieldSelector(),
		ksailconfigmanager.OIDCCAFileFieldSelector(),
	)
}

// hideConfigOnlyFlags hides the config-loading flags that a command needs for
// defaults and validation but does not expose in its help (distribution,
// distribution-config, gitops-engine, local-registry). Shared by connect and
// diff so the hidden-flag list cannot drift between them.
func hideConfigOnlyFlags(cmd *cobra.Command) {
	for _, flagName := range []string{
		"distribution", "distribution-config", "gitops-engine", "local-registry",
	} {
		if f := cmd.Flags().Lookup(flagName); f != nil {
			f.Hidden = true
		}
	}
}

// validatePostMutationFlags re-validates the configuration fields that
// clusterflags.ApplyClusterMutationFlags can change: OIDC extra scopes and Hetzner allowed
// CIDRs. Shared by create and update so the two commands cannot drift.
func validatePostMutationFlags(ctx *localregistry.Context) error {
	// Re-validate OIDC after merging CLI scope flags which can change ExtraScopes
	err := v1alpha1.ValidateOIDCConfig(&ctx.ClusterCfg.Spec.Cluster.OIDC)
	if err != nil {
		return fmt.Errorf("OIDC configuration: %w", err)
	}

	// Validate allowed CIDRs after merging CLI flags
	err = v1alpha1.ValidateAllowedCIDRs(ctx.ClusterCfg.Spec.Provider.Hetzner.AllowedCIDRs)
	if err != nil {
		return fmt.Errorf("allowed CIDRs configuration: %w", err)
	}

	return nil
}

// setupMutationCmdFlags creates the shared config manager and registers the
// common flags (--mirror-registry and --name) used by cluster mutation commands.
// Returns the config manager for further flag bindings.
func setupMutationCmdFlags(cmd *cobra.Command) *ksailconfigmanager.ConfigManager {
	cfgManager := ksailconfigmanager.NewCommandConfigManager(
		cmd,
		defaultClusterMutationFieldSelectors(),
	)

	clusterflags.RegisterMirrorRegistryFlag(cmd)
	clusterflags.RegisterNameFlag(cmd, cfgManager)
	clusterflags.RegisterOIDCExtraScopeFlag(cmd)
	clusterflags.RegisterAllowedCIDRsFlag(cmd)

	return cfgManager
}

// loadAndValidateClusterConfig loads configuration, applies name override, and validates
// the distribution x provider combination. This shared sequence is used by both
// create and update commands.
func loadAndValidateClusterConfig(
	cfgManager *ksailconfigmanager.ConfigManager,
	deps lifecycle.Deps,
) (*localregistry.Context, string, error) {
	outputTimer := deps.Timer

	ctx, err := loadClusterConfiguration(cfgManager, outputTimer)
	if err != nil {
		return nil, "", err
	}

	nameOverride, err := resolveMutationNameOverride(cfgManager, ctx)
	if err != nil {
		return nil, "", err
	}

	if nameOverride != "" {
		err = applyClusterNameOverride(ctx, nameOverride)
		if err != nil {
			return nil, "", err
		}
	}

	// Validate distribution x provider combination
	err = ctx.ClusterCfg.Spec.Cluster.Provider.ValidateForDistribution(
		ctx.ClusterCfg.Spec.Cluster.Distribution,
	)
	if err != nil {
		return nil, "", fmt.Errorf("invalid configuration: %w", err)
	}

	// Validate autoscaler configuration (pool names, min/max, server limit)
	err = v1alpha1.ValidateAutoscalerConfig(
		&ctx.ClusterCfg.Spec.Cluster,
		&ctx.ClusterCfg.Spec.Provider,
	)
	if err != nil {
		return nil, "", fmt.Errorf("invalid autoscaler configuration: %w", err)
	}

	// Validate OIDC configuration
	err = v1alpha1.ValidateOIDCConfig(&ctx.ClusterCfg.Spec.Cluster.OIDC)
	if err != nil {
		return nil, "", fmt.Errorf("OIDC configuration: %w", err)
	}

	// Validate EKS options here, before any billable provisioning: a
	// deterministically-invalid value must not first surface in the
	// post-create component setup (the installer re-checks as defense in depth).
	err = v1alpha1.ValidateEKSConfig(&ctx.ClusterCfg.Spec.Cluster)
	if err != nil {
		return nil, "", fmt.Errorf("EKS configuration: %w", err)
	}

	clusterName := resolveClusterNameFromContext(ctx)

	return ctx, clusterName, nil
}

// resolveMutationNameOverride validates the flag/metadata name priority before any distribution
// config is mutated. EKS is the special case: eksctl consumes the name in the on-disk eks.yaml, so
// an explicit contradiction must fail rather than be silently ignored in memory.
func resolveMutationNameOverride(
	cfgManager *ksailconfigmanager.ConfigManager,
	ctx *localregistry.Context,
) (string, error) {
	explicitName := cfgManager.Viper.GetString("name")
	if explicitName != "" {
		err := validateMutationClusterName(explicitName)
		if err != nil {
			return "", err
		}

		err = validateEKSNameOverride(ctx, explicitName)
		if err != nil {
			return "", err
		}

		return explicitName, nil
	}

	metadataName := ctx.ClusterCfg.Name
	if metadataName == "" {
		return "", nil
	}

	return metadataName, validateMutationClusterName(metadataName)
}

func validateMutationClusterName(name string) error {
	err := v1alpha1.ValidateClusterName(name)
	if err != nil {
		return fmt.Errorf("invalid cluster name %q: %w", name, err)
	}

	return nil
}

// validateEKSMutationConfigSource requires create/update to bind the target to the actual name in
// the eksctl source. Standalone lifecycle commands may instead use persisted KSail state, but an EKS
// create or recreation cannot safely infer the target because the provisioner passes only this file
// to eksctl and deliberately ignores the action's in-memory name argument.
func validateEKSMutationConfigSource(ctx *localregistry.Context) error {
	if ctx.ClusterCfg.Spec.Cluster.Distribution != v1alpha1.DistributionEKS {
		return nil
	}

	if ctx.EKSConfig == nil || strings.TrimSpace(ctx.EKSConfig.ConfigPath) == "" {
		return fmt.Errorf(
			"%w: provide a named eksctl ClusterConfig source",
			errEKSConfigurationUnavailable,
		)
	}

	sourceName := ctx.EKSConfig.Name

	canonicalName := strings.TrimSpace(sourceName)
	if !ctx.EKSConfig.NameFromConfig || canonicalName == "" {
		return fmt.Errorf(
			"%w in %s",
			errEKSMutationConfigNameNeeded,
			ctx.EKSConfig.ConfigPath,
		)
	}

	if sourceName != canonicalName {
		return fmt.Errorf(
			"%w: %q in %s has leading or trailing whitespace",
			errEKSMutationConfigNameInvalid,
			sourceName,
			ctx.EKSConfig.ConfigPath,
		)
	}

	err := v1alpha1.ValidateClusterName(sourceName)
	if err != nil {
		return fmt.Errorf("%w: %w", errEKSMutationConfigNameInvalid, err)
	}

	return nil
}

func validateEKSNameOverride(ctx *localregistry.Context, explicitName string) error {
	if ctx.ClusterCfg.Spec.Cluster.Distribution != v1alpha1.DistributionEKS ||
		ctx.EKSConfig == nil || strings.TrimSpace(ctx.EKSConfig.ConfigPath) == "" ||
		explicitName == strings.TrimSpace(ctx.EKSConfig.Name) {
		return nil
	}

	return fmt.Errorf(
		"%w: --name %q differs from %q in %s",
		errEKSNameOverrideMismatch,
		explicitName,
		strings.TrimSpace(ctx.EKSConfig.Name),
		ctx.EKSConfig.ConfigPath,
	)
}

// runClusterCreationWorkflow performs the full cluster creation workflow.
// This is the shared implementation used by both the create handler and
// the update command's recreate flow.
//
//nolint:funlen // Sequential workflow steps are clearer kept together
func runClusterCreationWorkflow(
	cmd *cobra.Command,
	cfgManager *ksailconfigmanager.ConfigManager,
	ctx *localregistry.Context,
	deps lifecycle.Deps,
) error {
	localDeps := getLocalRegistryDeps()

	err := ensureLocalRegistriesReady(
		cmd,
		ctx,
		deps,
		cfgManager,
		localDeps,
	)
	if err != nil {
		return err
	}

	setupK3dCNI(ctx.ClusterCfg, ctx.K3dConfig)
	setupK3dMetricsServer(ctx.ClusterCfg, ctx.K3dConfig)
	setupK3dCSI(ctx.ClusterCfg, ctx.K3dConfig)
	setupK3dLoadBalancer(ctx.ClusterCfg, ctx.K3dConfig)
	setupVClusterCNI(ctx.ClusterCfg, ctx.VClusterConfig)

	err = resolveNestedMirrorSpecs(cmd, cfgManager, ctx)
	if err != nil {
		return err
	}

	err = prepareEKSCreateIdentity(cmd.Context(), ctx)
	if err != nil {
		return err
	}

	configureProvisionerFactory(&deps, ctx)

	err = executeClusterCreationAndCapture(cmd, ctx, deps)
	if err != nil {
		return err
	}

	// Post-creation Docker steps are only needed for local Docker clusters.
	// Cloud providers (Omni, Hetzner) run nodes remotely and the Kubernetes
	// provider runs nodes as pods — neither can access local Docker infrastructure.
	if ctx.ClusterCfg.Spec.Cluster.Provider.NeedsLocalDocker() {
		configureRegistryMirrorsInClusterWithWarning(
			cmd,
			ctx,
			deps,
			cfgManager,
			localDeps,
		)

		err = localregistry.ExecuteStage(
			cmd,
			ctx,
			deps,
			localregistry.StageConnect,
			localDeps,
		)
		if err != nil {
			return fmt.Errorf("failed to connect local registry: %w", err)
		}
	}

	err = localregistry.WaitForK3dLocalRegistryReady(
		cmd,
		ctx.ClusterCfg,
		ctx.K3dConfig,
		localDeps.DockerInvoker,
	)
	if err != nil {
		return fmt.Errorf("failed to wait for local registry: %w", err)
	}

	// Set Connection.Context so post-CNI setup (InstallCNI, helm, kubectl) can resolve
	// the correct kubeconfig context. This MUST happen after local registry operations
	// (which resolve cluster name from distribution configs, not from context) but before
	// post-CNI setup (which needs the kubectl context name like "kind-kind").
	//
	// For Omni clusters, the kubeconfig context is now renamed during saveOmniKubeconfig
	// to match the configured context or the Talos convention (admin@<name>).
	// If an explicit context is already configured, preserve it.
	err = resolvePostCreateContext(ctx)
	if err != nil {
		return err
	}

	maybeImportCachedImages(cmd, ctx, deps.Timer)

	return handlePostCreationSetup(cmd, ctx.ClusterCfg, deps.Timer)
}

func prepareEKSCreateIdentity(
	ctx context.Context,
	clusterCtx *localregistry.Context,
) error {
	err := prepareEKSCreateConfig(clusterCtx)
	if err != nil {
		return err
	}

	return freezeEKSCreateCredentials(ctx, clusterCtx)
}

func executeClusterCreationAndCapture(
	cmd *cobra.Command,
	clusterCtx *localregistry.Context,
	deps lifecycle.Deps,
) error {
	err := executeClusterLifecycle(cmd, clusterCtx.ClusterCfg, deps)
	if err != nil {
		return err
	}

	return captureCreatedEKSIdentity(cmd.Context(), clusterCtx)
}

// prepareEKSCreateConfig resolves the effective AWS region and pins the
// kubeconfig path shared by eksctl creation and post-create setup.
func prepareEKSCreateConfig(ctx *localregistry.Context) error {
	if ctx.ClusterCfg.Spec.Cluster.Distribution != v1alpha1.DistributionEKS ||
		ctx.EKSConfig == nil {
		return nil
	}

	kubeconfigPath, err := kubeconfigutil.GetKubeconfigPathFromConfig(ctx.ClusterCfg)
	if err != nil {
		return fmt.Errorf("resolve EKS kubeconfig path: %w", err)
	}

	kubeconfigPath, err = prepareEKSOutputKubeconfigPath(kubeconfigPath)
	if err != nil {
		return err
	}

	ctx.EKSConfig.KubeconfigPath = kubeconfigPath
	ctx.EKSConfig.Region = lifecycle.ResolveAWSRegion(
		ctx.ClusterCfg.Spec.Provider.AWS,
		&clusterprovisioner.DistributionConfig{EKS: ctx.EKSConfig},
	)

	return nil
}

func freezeEKSCreateCredentials(
	ctx context.Context,
	clusterCtx *localregistry.Context,
) error {
	if clusterCtx.ClusterCfg.Spec.Cluster.Distribution != v1alpha1.DistributionEKS {
		return nil
	}

	if clusterCtx.EKSConfig == nil {
		return fmt.Errorf(
			"freeze AWS credentials for EKS creation: %w",
			errEKSConfigurationUnavailable,
		)
	}

	if clusterCtx.AWSResolution != nil && clusterCtx.AWSResolution.IsFrozen() {
		if region := strings.TrimSpace(clusterCtx.AWSResolution.Region); region != "" {
			clusterCtx.EKSConfig.Region = region
		}

		return nil
	}

	selection := credentials.ResolveAWS(
		credentials.NewAWSOptionsResolver(clusterCtx.ClusterCfg.Spec.Provider.AWS),
	)
	if clusterCtx.AWSResolution != nil {
		selection = *clusterCtx.AWSResolution
	}

	resolution, err := credentials.FreezeAWS(ctx, selection, clusterCtx.EKSConfig.Region)
	if err != nil {
		return fmt.Errorf("freeze AWS credentials for EKS creation: %w", err)
	}

	clusterCtx.AWSResolution = &resolution
	if region := strings.TrimSpace(resolution.Region); region != "" {
		clusterCtx.EKSConfig.Region = region
	}

	return nil
}

// captureCreatedEKSIdentity makes immutable ownership persistence part of EKS create success. It
// runs immediately after eksctl returns, before later setup or TTL can report success. The SDK
// client uses the same credential snapshot the provisioner factory received.
func captureCreatedEKSIdentity(
	ctx context.Context,
	clusterCtx *localregistry.Context,
) error {
	if clusterCtx.ClusterCfg.Spec.Cluster.Distribution != v1alpha1.DistributionEKS {
		return nil
	}

	if clusterCtx.EKSConfig == nil || clusterCtx.AWSResolution == nil {
		return fmt.Errorf(
			"capture immutable EKS ownership identity: %w",
			errEKSConfigurationUnavailable,
		)
	}

	eksIdentityClientFactoryState.RLock()
	factory := eksIdentityClientFactoryState.factory
	eksIdentityClientFactoryState.RUnlock()

	identityClient, err := factory(ctx, clusterCtx.EKSConfig.Region, *clusterCtx.AWSResolution)
	if err != nil {
		return fmt.Errorf("capture immutable EKS ownership identity: create AWS client: %w", err)
	}

	ownership, err := eksidentity.Capture(
		ctx,
		identityClient,
		strings.TrimSpace(clusterCtx.EKSConfig.Name),
		strings.TrimSpace(clusterCtx.EKSConfig.Region),
		credentials.AWSOptionsWithDefaults(clusterCtx.ClusterCfg.Spec.Provider.AWS),
	)
	if err != nil {
		return fmt.Errorf("capture immutable EKS ownership identity: %w", err)
	}

	clusterCtx.EKSConfig.Region = ownership.Region

	return nil
}

// prepareEKSOutputKubeconfigPath creates and canonicalizes the user-selected
// output location before passing it to the external eksctl process.
func prepareEKSOutputKubeconfigPath(kubeconfigPath string) (string, error) {
	err := os.MkdirAll(filepath.Dir(kubeconfigPath), eksKubeconfigDirMode)
	if err != nil {
		return "", fmt.Errorf("create EKS kubeconfig directory: %w", err)
	}

	canonicalPath, err := fsutil.EvalCanonicalPath(kubeconfigPath)
	if err != nil {
		return "", fmt.Errorf("canonicalize EKS kubeconfig path: %w", err)
	}

	return canonicalPath, nil
}

// resolvePostCreateContext selects the kubeconfig context created by the
// provisioner. Most distributions use a deterministic name, while eksctl
// prefixes EKS contexts with the AWS identity that created the cluster.
func resolvePostCreateContext(ctx *localregistry.Context) error {
	connection := &ctx.ClusterCfg.Spec.Cluster.Connection
	distribution := ctx.ClusterCfg.Spec.Cluster.Distribution

	if connection.Context != "" {
		if distribution == v1alpha1.DistributionEKS {
			return pinObservedExplicitEKSRegion(ctx, connection.Context)
		}

		return nil
	}

	if distribution != v1alpha1.DistributionEKS {
		connection.Context = resolveCreatedContextName(
			distribution,
			ctx.ClusterCfg.Spec.Cluster.Provider,
			resolveClusterNameFromContext(ctx),
		)

		return nil
	}

	return resolveEKSPostCreateContext(ctx)
}

// pinObservedExplicitEKSRegion derives a profile-selected region only from the kubeconfig output
// eksctl just made current. Parsing configured text alone could bind TTL cleanup to a stale
// same-named context in another region.
func pinObservedExplicitEKSRegion(ctx *localregistry.Context, contextName string) error {
	if ctx.EKSConfig != nil && strings.TrimSpace(ctx.EKSConfig.Region) != "" {
		return nil
	}

	clusterName, _, config, err := loadEKSContextConfig(ctx)
	if err != nil {
		return err
	}

	observedContext, found := config.Contexts[contextName]

	contextCluster, _, validTarget := parseEksctlContextTarget(contextName)
	if !found || observedContext == nil || config.CurrentContext != contextName ||
		!validTarget || contextCluster != clusterName {
		return fmt.Errorf(
			"%w: %q for cluster %q",
			errExplicitEKSContextUnobserved,
			contextName,
			clusterName,
		)
	}

	pinEKSRegionFromContext(ctx, contextName)

	if strings.TrimSpace(ctx.EKSConfig.Region) == "" {
		return fmt.Errorf(
			"%w: %q did not supply a region",
			errExplicitEKSContextUnobserved,
			contextName,
		)
	}

	return nil
}

// resolveEKSPostCreateContext selects the identity-qualified context that
// eksctl wrote for the configured cluster and region.
func resolveEKSPostCreateContext(ctx *localregistry.Context) error {
	clusterName, region, config, err := loadEKSContextConfig(ctx)
	if err != nil {
		return err
	}

	matches := make([]string, 0, len(config.Contexts))

	for contextName := range config.Contexts {
		if matchesEKSContext(contextName, clusterName, region) {
			matches = append(matches, contextName)
		}
	}

	sort.Strings(matches)

	selected, err := selectEKSContext(matches, config.CurrentContext, clusterName, region)
	if err != nil {
		return err
	}

	ctx.ClusterCfg.Spec.Cluster.Connection.Context = selected
	pinEKSRegionFromContext(ctx, selected)

	return nil
}

// pinEKSRegionFromContext preserves the exact region eksctl selected after a create whose region was
// delegated to the AWS profile. TTL and later lifecycle calls can then target that created region
// directly even when the kubeconfig also contains a same-named cluster elsewhere.
func pinEKSRegionFromContext(ctx *localregistry.Context, contextName string) {
	if ctx.EKSConfig == nil || strings.TrimSpace(ctx.EKSConfig.Region) != "" {
		return
	}

	clusterName, region, ok := parseEksctlContextTarget(contextName)
	if !ok || clusterName != strings.TrimSpace(ctx.EKSConfig.Name) {
		return
	}

	ctx.EKSConfig.Region = region
}

// loadEKSContextConfig loads the kubeconfig plus the cluster and effective
// region needed to select the identity-qualified eksctl context.
func loadEKSContextConfig(
	ctx *localregistry.Context,
) (string, string, *clientcmdapi.Config, error) {
	if ctx.EKSConfig == nil {
		return "", "", nil, fmt.Errorf(
			"resolve EKS kubeconfig context: %w; %s",
			errEKSConfigurationUnavailable,
			explicitEKSContextHint,
		)
	}

	clusterName := strings.TrimSpace(ctx.EKSConfig.Name)
	if clusterName == "" {
		return "", "", nil, fmt.Errorf(
			"resolve EKS kubeconfig context: %w; %s",
			errEKSClusterNameRequired,
			explicitEKSContextHint,
		)
	}

	kubeconfigPath, err := resolveEKSPostCreateKubeconfigPath(ctx)
	if err != nil {
		return "", "", nil, err
	}

	config, err := clientcmd.LoadFromFile(kubeconfigPath)
	if err != nil {
		return "", "", nil, fmt.Errorf("load EKS kubeconfig %q: %w", kubeconfigPath, err)
	}

	return clusterName, strings.TrimSpace(ctx.EKSConfig.Region), config, nil
}

// resolveEKSPostCreateKubeconfigPath returns the path pinned for eksctl,
// falling back to the loaded cluster configuration for direct helper callers.
func resolveEKSPostCreateKubeconfigPath(ctx *localregistry.Context) (string, error) {
	kubeconfigPath := strings.TrimSpace(ctx.EKSConfig.KubeconfigPath)
	if kubeconfigPath != "" {
		return kubeconfigPath, nil
	}

	path, err := kubeconfigutil.GetKubeconfigPathFromConfig(ctx.ClusterCfg)
	if err != nil {
		return "", fmt.Errorf("resolve EKS kubeconfig path: %w", err)
	}

	return path, nil
}

// selectEKSContext prefers the current matching context, then a unique match,
// and otherwise fails closed with an explicit-context recovery hint.
func selectEKSContext(matches []string, current, clusterName, region string) (string, error) {
	if slices.Contains(matches, current) {
		return current, nil
	}

	if len(matches) == 1 {
		return matches[0], nil
	}

	if len(matches) == 0 {
		return "", newEKSContextSelectionError(
			errNoMatchingEKSContext,
			clusterName,
			region,
			nil,
		)
	}

	return "", newEKSContextSelectionError(
		errAmbiguousEKSContext,
		clusterName,
		region,
		matches,
	)
}

// newEKSContextSelectionError formats a fail-closed selection error while
// retaining a static cause for errors.Is callers and err113 compliance.
func newEKSContextSelectionError(
	cause error,
	clusterName, region string,
	matches []string,
) error {
	if len(matches) > 0 && region != "" {
		return fmt.Errorf(
			"%w %q in region %q: %v; %s",
			cause,
			clusterName,
			region,
			matches,
			explicitEKSContextHint,
		)
	}

	if len(matches) > 0 {
		return fmt.Errorf("%w %q: %v; %s", cause, clusterName, matches, explicitEKSContextHint)
	}

	if region != "" {
		return fmt.Errorf(
			"%w %q in region %q; %s",
			cause,
			clusterName,
			region,
			explicitEKSContextHint,
		)
	}

	return fmt.Errorf("%w %q; %s", cause, clusterName, explicitEKSContextHint)
}

// matchesEKSContext reports whether an identity-qualified eksctl context
// targets the configured cluster and, when known, the effective AWS region.
func matchesEKSContext(contextName, clusterName, region string) bool {
	if region != "" {
		return strings.HasSuffix(
			contextName,
			"@"+clusterName+"."+region+".eksctl.io",
		)
	}

	marker := "@" + clusterName + "."

	markerIndex := strings.LastIndex(contextName, marker)
	if markerIndex < 0 || !strings.HasSuffix(contextName, ".eksctl.io") {
		return false
	}

	regionStart := markerIndex + len(marker)
	regionEnd := len(contextName) - len(".eksctl.io")

	return regionStart < regionEnd && !strings.Contains(contextName[regionStart:regionEnd], ".")
}

// resolveCreatedContextName returns the kubeconfig context name a freshly created
// cluster is written under, so post-creation setup (CNI install, helm, kubectl) can
// target it. The Kubernetes provider runs K3s via the k3k operator, which writes a
// "k3k-<name>" context rather than the standalone k3d "k3d-<name>" context; without
// this, installing a CNI like Calico on a nested K3s cluster fails to find the
// context. All other distribution/provider combinations use the standalone
// distribution context name.
func resolveCreatedContextName(
	distribution v1alpha1.Distribution,
	provider v1alpha1.Provider,
	clusterName string,
) string {
	if clusterName != "" &&
		provider == v1alpha1.ProviderKubernetes &&
		distribution == v1alpha1.DistributionK3s {
		return "k3k-" + clusterName
	}

	return distribution.ContextName(clusterName)
}
