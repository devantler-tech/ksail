package cluster

import (
	"fmt"
	"os"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/annotations"
	"github.com/devantler-tech/ksail/v7/pkg/cli/flags"
	"github.com/devantler-tech/ksail/v7/pkg/cli/setup/mirrorregistry"
	"github.com/devantler-tech/ksail/v7/pkg/di"
	configmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager"
	ksailconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/ksail"
	"github.com/devantler-tech/ksail/v7/pkg/fsutil/scaffolder"
	"github.com/devantler-tech/ksail/v7/pkg/notify"
	"github.com/devantler-tech/ksail/v7/pkg/svc/clusterdiscovery"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clustererr"
	"github.com/devantler-tech/ksail/v7/pkg/svc/state"
	"github.com/devantler-tech/ksail/v7/pkg/timer"
	"github.com/spf13/cobra"
)

// NewInitCmd creates and returns the init command.
func NewInitCmd(runtimeContainer *di.Runtime) *cobra.Command {
	cmd := &cobra.Command{
		Use:          "init",
		Short:        "Initialize a new project",
		Long:         "Initialize a new project in the specified directory (or current directory if none specified).",
		SilenceUsage: true,
		Annotations: map[string]string{
			annotations.AnnotationPermission: permissionWrite,
		},
	}

	cfgManager := ksailconfigmanager.NewCommandConfigManager(cmd, InitFieldSelectors())

	// Bind init-local flags (not part of shared cluster config). Keeping this scoped
	// here avoids polluting the generic config manager with scaffolding concerns.
	bindInitLocalFlags(cmd, cfgManager)

	cmd.RunE = di.RunEWithRuntime(
		runtimeContainer,
		di.WithTimer(func(cmd *cobra.Command, _ di.Injector, tmr timer.Timer) error {
			deps := InitDeps{Timer: tmr}

			return HandleInitRunE(cmd, cfgManager, deps)
		}),
	)

	return cmd
}

// InitFieldSelectors returns the field selectors used by the init command.
// Kept local (rather than separate file) to keep init-specific wiring cohesive.
func InitFieldSelectors() []ksailconfigmanager.FieldSelector[v1alpha1.Cluster] {
	selectors := ksailconfigmanager.DefaultClusterFieldSelectors()
	selectors = append(selectors, ksailconfigmanager.DefaultProviderFieldSelector())
	selectors = append(selectors, ksailconfigmanager.StandardSourceDirectoryFieldSelector())
	selectors = append(selectors, ksailconfigmanager.StandardKustomizationFileFieldSelector())
	selectors = append(selectors, ksailconfigmanager.DefaultCNIFieldSelector())
	selectors = append(selectors, ksailconfigmanager.DefaultCSIFieldSelector())
	selectors = append(selectors, ksailconfigmanager.DefaultCDIFieldSelector())
	selectors = append(selectors, ksailconfigmanager.DefaultMetricsServerFieldSelector())
	selectors = append(selectors, ksailconfigmanager.DefaultLoadBalancerFieldSelector())
	selectors = append(selectors, ksailconfigmanager.DefaultCertManagerFieldSelector())
	selectors = append(selectors, ksailconfigmanager.DefaultPolicyEngineFieldSelector())
	selectors = append(selectors, ksailconfigmanager.DefaultImportImagesFieldSelector())
	// Unified node count selectors for all distributions
	selectors = append(selectors, ksailconfigmanager.ControlPlanesFieldSelector())
	selectors = append(selectors, ksailconfigmanager.WorkersFieldSelector())
	selectors = append(
		selectors,
		ksailconfigmanager.NodeAutoscalingFieldSelector(), //nolint:staticcheck
	)
	selectors = append(selectors, ksailconfigmanager.NodeAutoscalerEnabledFieldSelector())
	// Talos-specific selectors
	selectors = append(selectors, ksailconfigmanager.ImageVerificationFieldSelector())

	// OIDC authentication selectors
	selectors = append(selectors, ksailconfigmanager.OIDCIssuerURLFieldSelector())
	selectors = append(selectors, ksailconfigmanager.OIDCClientIDFieldSelector())
	selectors = append(selectors, ksailconfigmanager.OIDCUsernameClaimFieldSelector())
	selectors = append(selectors, ksailconfigmanager.OIDCUsernamePrefixFieldSelector())
	selectors = append(selectors, ksailconfigmanager.OIDCGroupsClaimFieldSelector())
	selectors = append(selectors, ksailconfigmanager.OIDCGroupsPrefixFieldSelector())
	selectors = append(selectors, ksailconfigmanager.OIDCCAFileFieldSelector())

	return selectors
}

// bindInitLocalFlags adds and binds flags that are specific to the init command only.
// They intentionally do not belong to the shared cluster field selectors.
func bindInitLocalFlags(cmd *cobra.Command, cfgManager *ksailconfigmanager.ConfigManager) {
	cmd.Flags().StringP("output", "o", "", "Output directory for the project")
	_ = cfgManager.Viper.BindPFlag("output", cmd.Flags().Lookup("output"))
	cmd.Flags().BoolP("force", "f", false, "Overwrite existing files")
	_ = cfgManager.Viper.BindPFlag("force", cmd.Flags().Lookup("force"))
	cmd.Flags().StringSlice(
		"mirror-registry",
		[]string{},
		"Configure mirror registries with optional authentication. Format: [user:pass@]host[=upstream]. "+
			"Credentials support environment variables using ${VAR} syntax (quote placeholders so KSail can expand them). "+
			"Examples: docker.io=https://registry-1.docker.io, '${USER}:${TOKEN}@ghcr.io=https://ghcr.io'",
	)
	// NOTE: mirror-registry is NOT bound to Viper to allow custom merge logic
	// It's handled manually in mirrorregistry.GetMirrorRegistriesWithDefaults()
	cmd.Flags().StringP(
		"name",
		"n",
		"",
		"Cluster name used for container names, registry names, and kubeconfig context",
	)
	_ = cfgManager.Viper.BindPFlag("name", cmd.Flags().Lookup("name"))

	registerOIDCExtraScopeFlag(cmd)
	registerAllowedCIDRsFlag(cmd)
}

// InitDeps holds dependencies injected into HandleInitRunE.
type InitDeps struct {
	Timer timer.Timer
}

// validateInitConfig validates the cluster configuration for the init command.
func validateInitConfig(clusterCfg *v1alpha1.Cluster) error {
	// Early validation of distribution x provider combination
	err := clusterCfg.Spec.Cluster.Provider.ValidateForDistribution(
		clusterCfg.Spec.Cluster.Distribution,
	)
	if err != nil {
		return fmt.Errorf("invalid configuration: %w", err)
	}

	// Validate local registry configuration for the provider
	err = v1alpha1.ValidateLocalRegistryForProvider(
		clusterCfg.Spec.Cluster.Provider,
		clusterCfg.Spec.Cluster.LocalRegistry,
	)
	if err != nil {
		return fmt.Errorf("local registry validation: %w", err)
	}

	// Validate OIDC configuration
	err = v1alpha1.ValidateOIDCConfig(&clusterCfg.Spec.Cluster.OIDC)
	if err != nil {
		return fmt.Errorf("OIDC configuration: %w", err)
	}

	return nil
}

// validatePostFlagInitConfig validates config fields that may have been modified by CLI flags.
func validatePostFlagInitConfig(clusterCfg *v1alpha1.Cluster) error {
	err := v1alpha1.ValidateOIDCConfig(&clusterCfg.Spec.Cluster.OIDC)
	if err != nil {
		return fmt.Errorf("OIDC configuration: %w", err)
	}

	err = v1alpha1.ValidateAllowedCIDRs(clusterCfg.Spec.Provider.Hetzner.AllowedCIDRs)
	if err != nil {
		return fmt.Errorf("allowed CIDRs configuration: %w", err)
	}

	return nil
}

// HandleInitRunE handles the init command.
func HandleInitRunE(
	cmd *cobra.Command,
	cfgManager *ksailconfigmanager.ConfigManager,
	deps InitDeps,
) error {
	if deps.Timer != nil {
		deps.Timer.Start()
	}

	clusterCfg, err := cfgManager.Load(
		configmanager.LoadOptions{Silent: true, IgnoreConfigFile: true},
	)
	if err != nil {
		return fmt.Errorf("failed to resolve configuration for scaffolding: %w", err)
	}

	err = validateInitConfig(clusterCfg)
	if err != nil {
		return err
	}

	applyOIDCExtraScopeFlag(cmd, clusterCfg)
	applyAllowedCIDRsFlag(cmd, clusterCfg)

	err = validatePostFlagInitConfig(clusterCfg)
	if err != nil {
		return err
	}

	scaffolderInstance, targetPath, force, err := prepareScaffolder(cmd, cfgManager, clusterCfg)
	if err != nil {
		return err
	}

	if deps.Timer != nil {
		deps.Timer.NewStage()
	}

	notify.WriteMessage(notify.Message{
		Type:    notify.TitleType,
		Content: "Initialize project...",
		Emoji:   "📂",
		Writer:  cmd.OutOrStdout(),
	})

	err = scaffolderInstance.Scaffold(targetPath, force)
	if err != nil {
		return fmt.Errorf("failed to scaffold project files: %w", err)
	}

	notify.WriteMessage(notify.Message{
		Type:    notify.SuccessType,
		Content: "initialized project",
		Timer:   flags.MaybeTimer(cmd, deps.Timer),
		Writer:  cmd.OutOrStdout(),
	})

	return nil
}

// prepareScaffolder sets up the scaffolder with configuration from flags.
func prepareScaffolder(
	cmd *cobra.Command,
	cfgManager *ksailconfigmanager.ConfigManager,
	clusterCfg *v1alpha1.Cluster,
) (*scaffolder.Scaffolder, string, bool, error) {
	targetPath, err := resolveInitTargetPath(cfgManager)
	if err != nil {
		return nil, "", false, err
	}

	force := cfgManager.Viper.GetBool("force")
	mirrorRegistries := mirrorregistry.GetMirrorRegistriesWithDefaults(
		cmd, cfgManager, clusterCfg.Spec.Cluster.Provider,
	)
	clusterName := cfgManager.Viper.GetString("name")

	// Validate mirror registries are compatible with the provider
	err = v1alpha1.ValidateMirrorRegistriesForProvider(
		clusterCfg.Spec.Cluster.Provider,
		mirrorRegistries,
	)
	if err != nil {
		return nil, "", false, fmt.Errorf("invalid configuration: %w", err)
	}

	// Validate cluster name is DNS-1123 compliant
	if clusterName != "" {
		validationErr := v1alpha1.ValidateClusterName(clusterName)
		if validationErr != nil {
			return nil, "", false, fmt.Errorf("invalid --name flag: %w", validationErr)
		}
	}

	scaffolderInstance := scaffolder.NewScaffolder(
		*clusterCfg,
		cmd.OutOrStdout(),
		mirrorRegistries,
	)

	// Apply cluster name override if provided
	if clusterName != "" {
		scaffolderInstance.WithClusterName(clusterName)
	}

	return scaffolderInstance, targetPath, force, nil
}

func resolveInitTargetPath(cfgManager *ksailconfigmanager.ConfigManager) (string, error) {
	flagOutputPath := cfgManager.Viper.GetString("output")
	if flagOutputPath != "" {
		return flagOutputPath, nil
	}

	wd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get current directory: %w", err)
	}

	return wd, nil
}

// ErrUnsupportedProvider re-exports the shared error for backward compatibility.
var ErrUnsupportedProvider = clustererr.ErrUnsupportedProvider

// allDistributions returns the Docker-based distributions enumerated when listing local clusters.
// It delegates to clusterdiscovery so the CLI and the web UI share one source of truth.
func allDistributions() []v1alpha1.Distribution {
	return clusterdiscovery.LocalDistributions()
}

// allProviders returns the providers `ksail cluster list` queries by default (Docker, Hetzner,
// Omni). It delegates to clusterdiscovery.DefaultProviders.
func allProviders() []v1alpha1.Provider {
	return clusterdiscovery.DefaultProviders()
}

// listResult holds a cluster name with its provider and distribution for display purposes.
type listResult struct {
	Provider     v1alpha1.Provider
	Distribution v1alpha1.Distribution
	ClusterName  string
	TTL          *state.TTLInfo // nil if no TTL has been set for this cluster
}

// tableColumnGap is the minimum gap between columns in table output.
const tableColumnGap = 3
