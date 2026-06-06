package cluster

import (
	"fmt"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/lifecycle"
	"github.com/devantler-tech/ksail/v7/pkg/cli/setup/localregistry"
	ksailconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/ksail"
	"github.com/spf13/cobra"
)

// defaultClusterMutationFieldSelectors returns the full set of field selectors
// used by commands that modify cluster state (create, update).
// This centralizes the selector list to avoid duplication between commands.
func defaultClusterMutationFieldSelectors() []ksailconfigmanager.FieldSelector[v1alpha1.Cluster] {
	selectors := ksailconfigmanager.DefaultClusterFieldSelectors()

	return append(
		selectors,
		ksailconfigmanager.DefaultProviderFieldSelector(),
		ksailconfigmanager.DefaultCNIFieldSelector(),
		ksailconfigmanager.DefaultMetricsServerFieldSelector(),
		ksailconfigmanager.DefaultLoadBalancerFieldSelector(),
		ksailconfigmanager.DefaultCertManagerFieldSelector(),
		ksailconfigmanager.DefaultPolicyEngineFieldSelector(),
		ksailconfigmanager.DefaultCSIFieldSelector(),
		ksailconfigmanager.DefaultCDIFieldSelector(),
		ksailconfigmanager.DefaultImportImagesFieldSelector(),
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

// registerMirrorRegistryFlag adds the --mirror-registry flag to a command.
// The flag is intentionally NOT bound to Viper to allow custom merge logic
// via getMirrorRegistriesWithDefaults() in setup/mirrorregistry.
func registerMirrorRegistryFlag(cmd *cobra.Command) {
	cmd.Flags().StringSlice("mirror-registry", []string{},
		"Configure mirror registries with optional authentication. Format: [user:pass@]host[=upstream]. "+
			"Credentials support environment variables using ${VAR} syntax (quote placeholders so KSail can expand them). "+
			"Examples: docker.io=https://registry-1.docker.io, '${USER}:${TOKEN}@ghcr.io=https://ghcr.io'")
}

// registerNameFlag adds the --name flag to a command and binds it to Viper.
func registerNameFlag(cmd *cobra.Command, cfgManager *ksailconfigmanager.ConfigManager) {
	cmd.Flags().StringP("name", "n", "",
		"Cluster name used for container names, registry names, and kubeconfig context")
	_ = cfgManager.Viper.BindPFlag("name", cmd.Flags().Lookup("name"))
}

// registerOIDCExtraScopeFlag adds the --oidc-extra-scope flag to a command.
// Like --mirror-registry, this is a string slice flag that is NOT bound to Viper
// and instead merged manually after config loading.
func registerOIDCExtraScopeFlag(cmd *cobra.Command) {
	cmd.Flags().StringSlice("oidc-extra-scope", []string{},
		"Additional OIDC scopes beyond openid (repeatable)")
}

// applyOIDCExtraScopeFlag merges --oidc-extra-scope flag values into the cluster config.
// CLI flag values take precedence over config file values when explicitly set.
func applyOIDCExtraScopeFlag(cmd *cobra.Command, clusterCfg *v1alpha1.Cluster) {
	scopeFlag := cmd.Flags().Lookup("oidc-extra-scope")
	if scopeFlag == nil || !scopeFlag.Changed {
		return
	}

	scopes, err := cmd.Flags().GetStringSlice("oidc-extra-scope")
	if err != nil {
		return
	}

	// When the flag is explicitly set, always assign — even if empty — so the
	// user can clear extraScopes from a config file via CLI.
	clusterCfg.Spec.Cluster.OIDC.ExtraScopes = scopes
}

// registerAllowedCIDRsFlag adds the --allowed-cidrs flag to a command.
// Like --mirror-registry, this is a string slice flag NOT bound to Viper
// and merged manually via applyAllowedCIDRsFlag.
func registerAllowedCIDRsFlag(cmd *cobra.Command) {
	cmd.Flags().StringSlice("allowed-cidrs", []string{},
		"CIDR blocks allowed to access the Kubernetes API and Talos API on control-plane nodes. "+
			"When empty, both APIs are open to 0.0.0.0/0 and ::/0 (all IPv4 and IPv6). "+
			"Example: --allowed-cidrs 203.0.113.0/24 --allowed-cidrs 198.51.100.0/24")
}

// applyAllowedCIDRsFlag merges --allowed-cidrs flag values into the cluster config.
// CLI flag values take precedence over config file values when explicitly set.
func applyAllowedCIDRsFlag(cmd *cobra.Command, clusterCfg *v1alpha1.Cluster) {
	cidrFlag := cmd.Flags().Lookup("allowed-cidrs")
	if cidrFlag == nil || !cidrFlag.Changed {
		return
	}

	cidrs, err := cmd.Flags().GetStringSlice("allowed-cidrs")
	if err != nil {
		return
	}

	clusterCfg.Spec.Provider.Hetzner.AllowedCIDRs = cidrs
}

// applyClusterMutationFlags merges the non-Viper CLI flag overrides
// (--oidc-extra-scope and --allowed-cidrs) into the cluster config. Centralizing
// the set keeps every mutation command (create, update, init) applying the same
// flags; a new manually-merged flag is added here once rather than at each call site.
func applyClusterMutationFlags(cmd *cobra.Command, clusterCfg *v1alpha1.Cluster) {
	applyOIDCExtraScopeFlag(cmd, clusterCfg)
	applyAllowedCIDRsFlag(cmd, clusterCfg)
}

// setupMutationCmdFlags creates the shared config manager and registers the
// common flags (--mirror-registry and --name) used by cluster mutation commands.
// Returns the config manager for further flag bindings.
func setupMutationCmdFlags(cmd *cobra.Command) *ksailconfigmanager.ConfigManager {
	cfgManager := ksailconfigmanager.NewCommandConfigManager(
		cmd,
		defaultClusterMutationFieldSelectors(),
	)

	registerMirrorRegistryFlag(cmd)
	registerNameFlag(cmd, cfgManager)
	registerOIDCExtraScopeFlag(cmd)
	registerAllowedCIDRsFlag(cmd)

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
	if ctx.ClusterCfg.Spec.Cluster.Connection.Context == "" {
		clusterName := resolveClusterNameFromContext(ctx)
		ctx.ClusterCfg.Spec.Cluster.Connection.Context = resolveCreatedContextName(
			ctx.ClusterCfg.Spec.Cluster.Distribution,
			ctx.ClusterCfg.Spec.Cluster.Provider,
			clusterName,
		)
	}

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
