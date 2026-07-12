package cluster

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/clusterflags"
	"github.com/devantler-tech/ksail/v7/pkg/cli/lifecycle"
	"github.com/devantler-tech/ksail/v7/pkg/cli/setup/localregistry"
	ksailconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/ksail"
	"github.com/devantler-tech/ksail/v7/pkg/k8s"
	"github.com/spf13/cobra"
	"k8s.io/client-go/tools/clientcmd"
)

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

	// Apply cluster name override: --name flag takes priority, then metadata.name
	nameOverride := cfgManager.Viper.GetString("name")
	if nameOverride == "" {
		nameOverride = ctx.ClusterCfg.Name
	}

	if nameOverride != "" {
		validationErr := v1alpha1.ValidateClusterName(nameOverride)
		if validationErr != nil {
			return nil, "", fmt.Errorf("invalid cluster name %q: %w", nameOverride, validationErr)
		}

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

	clusterName := resolveClusterNameFromContext(ctx)

	return ctx, clusterName, nil
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

	configureProvisionerFactory(&deps, ctx)

	err = executeClusterLifecycle(cmd, ctx.ClusterCfg, deps)
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
	createdContext, err := resolveCreatedContext(ctx, resolveClusterNameFromContext(ctx))
	if err != nil {
		return err
	}

	ctx.ClusterCfg.Spec.Cluster.Connection.Context = createdContext

	maybeImportCachedImages(cmd, ctx, deps.Timer)

	return handlePostCreationSetup(cmd, ctx.ClusterCfg, deps.Timer)
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

// ErrEKSContextNotFound is returned when no kubeconfig context matches the
// freshly created EKS cluster, so post-create GitOps bootstrapping would
// target a context that does not exist.
var ErrEKSContextNotFound = errors.New(
	"no kubeconfig context matches the created EKS cluster; " +
		"set spec.cluster.connection.context to the context eksctl wrote",
)

// ErrEKSContextAmbiguous is returned when more than one kubeconfig context
// matches the freshly created EKS cluster and none of them is current-context.
var ErrEKSContextAmbiguous = errors.New(
	"multiple kubeconfig contexts match the created EKS cluster; " +
		"set spec.cluster.connection.context to the context eksctl wrote",
)

// resolveCreatedContext returns the kubeconfig context post-creation setup
// (CNI install, helm, Flux/ArgoCD bootstrap) should target. An explicitly
// configured context is always preserved unchanged. Most distributions write
// a deterministic context name (resolveCreatedContextName); EKS is the
// exception — eksctl qualifies the context with the caller's IAM identity as
// "<identity>@<name>.<region>.eksctl.io", and the identity is unknown until
// eksctl has written the kubeconfig, so the actual context is read back from
// the configured kubeconfig after creation.
func resolveCreatedContext(
	ctx *localregistry.Context,
	clusterName string,
) (string, error) {
	cluster := &ctx.ClusterCfg.Spec.Cluster
	if cluster.Connection.Context != "" {
		return cluster.Connection.Context, nil
	}

	if cluster.Distribution == v1alpha1.DistributionEKS {
		region := ""
		if ctx.EKSConfig != nil {
			region = ctx.EKSConfig.Region
		}

		return resolveEKSCreatedContext(cluster.Connection.Kubeconfig, clusterName, region)
	}

	return resolveCreatedContextName(cluster.Distribution, cluster.Provider, clusterName), nil
}

// resolveEKSCreatedContext reads the kubeconfig eksctl just wrote and resolves
// the identity-qualified context ("<identity>@<name>.<region>.eksctl.io") for
// the created cluster. A matching current-context wins; otherwise exactly one
// matching context is accepted. Zero or multiple ambiguous matches fail closed
// with a hint to configure the context explicitly, rather than letting the
// GitOps bootstrap target a context that does not exist.
func resolveEKSCreatedContext(kubeconfigSpec, name, region string) (string, error) {
	path, err := k8s.ResolveKubeconfigPath(kubeconfigSpec)
	if err != nil {
		return "", fmt.Errorf("resolve kubeconfig path: %w", err)
	}

	config, err := clientcmd.LoadFromFile(path)
	if err != nil {
		return "", fmt.Errorf("read kubeconfig after EKS creation: %w", err)
	}

	if config.CurrentContext != "" && eksContextMatches(config.CurrentContext, name, region) {
		return config.CurrentContext, nil
	}

	matches := make([]string, 0, 1)

	for contextName := range config.Contexts {
		if eksContextMatches(contextName, name, region) {
			matches = append(matches, contextName)
		}
	}

	switch len(matches) {
	case 1:
		return matches[0], nil
	case 0:
		return "", fmt.Errorf(
			"%w: no context matching %q in %q",
			ErrEKSContextNotFound, "@"+name+"."+region+".eksctl.io", path,
		)
	default:
		sort.Strings(matches)

		return "", fmt.Errorf(
			"%w: candidates %s in %q",
			ErrEKSContextAmbiguous, strings.Join(matches, ", "), path,
		)
	}
}

// eksContextMatches reports whether an existing kubeconfig context name is the
// eksctl-written context for the given cluster name and region. eksctl writes
// "<iam-identity>@<name>.<region>.eksctl.io"; the identity part is arbitrary.
// When the region is unknown (no parsed eks.yaml metadata), any region segment
// is accepted.
func eksContextMatches(contextName, name, region string) bool {
	atIdx := strings.LastIndex(contextName, "@")
	if atIdx < 0 || atIdx+1 >= len(contextName) {
		return false
	}

	qualifier := contextName[atIdx+1:]
	if region != "" {
		return qualifier == name+"."+region+".eksctl.io"
	}

	rest, found := strings.CutSuffix(qualifier, ".eksctl.io")
	if !found {
		return false
	}

	return strings.HasPrefix(rest, name+".")
}
