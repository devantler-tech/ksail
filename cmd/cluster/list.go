package cluster

import (
	"fmt"
	"io"
	"strings"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	runtime "github.com/devantler-tech/ksail/v5/pkg/di"
	ksailconfigmanager "github.com/devantler-tech/ksail/v5/pkg/io/config-manager/ksail"
	talosconfigmanager "github.com/devantler-tech/ksail/v5/pkg/io/config-manager/talos"
	clusterprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster"
	"github.com/devantler-tech/ksail/v5/pkg/ui/notify"
	k3dv1alpha5 "github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
	"github.com/spf13/cobra"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

const allFlag = "all"

// NewListCmd creates the list command for clusters.
func NewListCmd(runtimeContainer *runtime.Runtime) *cobra.Command {
	cmd := &cobra.Command{
		Use:          "list",
		Short:        "List clusters",
		Long:         `List all Kubernetes clusters managed by KSail.`,
		SilenceUsage: true,
	}

	cfgManager := ksailconfigmanager.NewCommandConfigManager(
		cmd,
		ksailconfigmanager.DefaultClusterFieldSelectors(),
	)

	bindAllFlag(cmd, cfgManager)

	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		return runtimeContainer.Invoke(func(_ runtime.Injector) error {
			deps := ListDeps{}

			return HandleListRunE(cmd, cfgManager, deps)
		})
	}

	return cmd
}

// ListDeps captures dependencies needed for the list command logic.
type ListDeps struct {
	// DistributionFactoryCreator is an optional function that creates factories for distributions.
	// If nil, real factories with empty configs are used.
	// This is primarily for testing purposes.
	DistributionFactoryCreator func(v1alpha1.Distribution) clusterprovisioner.Factory
}

// HandleListRunE handles the list command.
// Exported for testing purposes.
func HandleListRunE(
	cmd *cobra.Command,
	cfgManager *ksailconfigmanager.ConfigManager,
	deps ListDeps,
) error {
	// Load cluster configuration
	_, err := cfgManager.LoadConfigSilent()
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	// List clusters
	err = listClusters(cfgManager, deps, cmd)
	if err != nil {
		return fmt.Errorf("failed to list clusters: %w", err)
	}

	return nil
}

func listClusters(
	cfgManager *ksailconfigmanager.ConfigManager,
	deps ListDeps,
	cmd *cobra.Command,
) error {
	clusterCfg := cfgManager.Config
	includeDistribution := cfgManager.Viper.GetBool(allFlag)

	primaryErr := listPrimaryClusters(cmd, clusterCfg, deps, includeDistribution)
	if primaryErr != nil {
		return primaryErr
	}

	if !includeDistribution {
		return nil
	}

	return listAdditionalDistributionClusters(cmd, clusterCfg, deps)
}

func listPrimaryClusters(
	cmd *cobra.Command,
	clusterCfg *v1alpha1.Cluster,
	deps ListDeps,
	includeDistribution bool,
) error {
	// Create a factory with an empty config for the distribution.
	// For list operations, we only need the provisioner type, not specific config data.
	var factory clusterprovisioner.Factory
	if deps.DistributionFactoryCreator != nil {
		factory = deps.DistributionFactoryCreator(clusterCfg.Spec.Cluster.Distribution)
	} else {
		factory = clusterprovisioner.DefaultFactory{
			DistributionConfig: createEmptyDistributionConfig(clusterCfg.Spec.Cluster.Distribution),
		}
	}

	provisioner, _, err := factory.Create(cmd.Context(), clusterCfg)
	if err != nil {
		return fmt.Errorf("failed to resolve cluster provisioner: %w", err)
	}

	clusters, err := provisioner.List(cmd.Context())
	if err != nil {
		return fmt.Errorf("failed to list clusters: %w", err)
	}

	displayClusterList(clusterCfg.Spec.Cluster.Distribution, clusters, cmd, includeDistribution)

	return nil
}

func listAdditionalDistributionClusters(
	cmd *cobra.Command,
	clusterCfg *v1alpha1.Cluster,
	deps ListDeps,
) error {
	for _, distribution := range []v1alpha1.Distribution{
		v1alpha1.DistributionKind,
		v1alpha1.DistributionK3d,
		v1alpha1.DistributionTalos,
	} {
		if distribution == clusterCfg.Spec.Cluster.Distribution {
			continue
		}

		listErr := listDistributionClusters(cmd, clusterCfg, deps, distribution)
		if listErr != nil {
			return listErr
		}
	}

	return nil
}

func listDistributionClusters(
	cmd *cobra.Command,
	clusterCfg *v1alpha1.Cluster,
	deps ListDeps,
	distribution v1alpha1.Distribution,
) error {
	otherCluster := cloneClusterForDistribution(clusterCfg, distribution)
	if otherCluster == nil {
		return nil
	}

	// Use custom factory creator if provided (for testing), otherwise create real factory.
	var distributionFactory clusterprovisioner.Factory
	if deps.DistributionFactoryCreator != nil {
		distributionFactory = deps.DistributionFactoryCreator(distribution)
	} else {
		// Create a factory with an empty config for the distribution.
		// For list operations, we only need the provisioner type, not specific config data.
		distributionFactory = clusterprovisioner.DefaultFactory{
			DistributionConfig: createEmptyDistributionConfig(distribution),
		}
	}

	otherProv, _, err := distributionFactory.Create(cmd.Context(), otherCluster)
	if err != nil {
		return fmt.Errorf(
			"failed to create provisioner for distribution %s: %w",
			distribution,
			err,
		)
	}

	otherClusters, err := otherProv.List(cmd.Context())
	if err != nil {
		return fmt.Errorf(
			"failed to list clusters for distribution %s: %w",
			distribution,
			err,
		)
	}

	displayClusterList(distribution, otherClusters, cmd, true)

	return nil
}

func cloneClusterForDistribution(
	original *v1alpha1.Cluster,
	distribution v1alpha1.Distribution,
) *v1alpha1.Cluster {
	if original == nil {
		return nil
	}

	clone := *original
	clone.Spec = original.Spec
	clone.Spec.Cluster.Distribution = distribution

	if distribution != original.Spec.Cluster.Distribution {
		clone.Spec.Cluster.DistributionConfig = defaultDistributionConfigPath(distribution)
	}

	return &clone
}

func defaultDistributionConfigPath(distribution v1alpha1.Distribution) string {
	switch distribution {
	case v1alpha1.DistributionKind:
		return "kind.yaml"
	case v1alpha1.DistributionK3d:
		return "k3d.yaml"
	case v1alpha1.DistributionTalos:
		return talosconfigmanager.DefaultPatchesDir
	default:
		return "kind.yaml"
	}
}

// createEmptyDistributionConfig creates an empty distribution config for the given distribution.
// This is used for list operations where we only need the provisioner type, not specific config data.
func createEmptyDistributionConfig(
	distribution v1alpha1.Distribution,
) *clusterprovisioner.DistributionConfig {
	switch distribution {
	case v1alpha1.DistributionKind:
		return &clusterprovisioner.DistributionConfig{
			Kind: &v1alpha4.Cluster{},
		}
	case v1alpha1.DistributionK3d:
		return &clusterprovisioner.DistributionConfig{
			K3d: &k3dv1alpha5.SimpleConfig{},
		}
	case v1alpha1.DistributionTalos:
		return &clusterprovisioner.DistributionConfig{
			Talos: &talosconfigmanager.Configs{},
		}
	default:
		return &clusterprovisioner.DistributionConfig{
			Kind: &v1alpha4.Cluster{},
		}
	}
}

func displayClusterList(
	distribution v1alpha1.Distribution,
	clusters []string,
	cmd *cobra.Command,
	includeDistribution bool,
) {
	writer := cmd.OutOrStdout()

	// Add distribution header when showing all distributions
	if includeDistribution {
		_, _ = fmt.Fprintf(writer, "---|%s|---\n", strings.ToLower(string(distribution)))
	}

	if len(clusters) == 0 {
		displayEmptyClusters(distribution, includeDistribution, writer)
	} else {
		displayClusterNames(distribution, clusters, includeDistribution, writer)
	}

	// Add blank line after each distribution section when showing all
	if includeDistribution {
		_, _ = fmt.Fprintln(writer)
	}
}

func displayEmptyClusters(
	_ v1alpha1.Distribution,
	includeDistribution bool,
	writer io.Writer,
) {
	// When showing all distributions, show distribution-specific empty messages
	if includeDistribution {
		_, _ = fmt.Fprintln(writer, "No clusters found.")
	}
	// When not showing all distributions, the provisioner already displays its own message
	// (e.g., "No kind clusters found."), so we don't display an additional message here.
}

func displayClusterNames(
	distribution v1alpha1.Distribution,
	clusters []string,
	includeDistribution bool,
	writer io.Writer,
) {
	var builder strings.Builder
	if includeDistribution {
		builder.WriteString(strings.ToLower(string(distribution)))
		builder.WriteString(": ")
	}

	builder.WriteString(strings.Join(clusters, ", "))
	builder.WriteString("\n")

	_, err := fmt.Fprint(writer, builder.String())
	if err != nil {
		notify.WriteMessage(notify.Message{
			Type:    notify.ErrorType,
			Content: fmt.Sprintf("failed to display %s clusters", distribution),
			Writer:  writer,
		})
	}
}

func bindAllFlag(cmd *cobra.Command, cfgManager *ksailconfigmanager.ConfigManager) {
	cmd.Flags().
		BoolP(allFlag, "a", false, "List all clusters, including those not defined in the configuration")
	flag := cmd.Flags().Lookup(allFlag)
	_ = cfgManager.Viper.BindPFlag(allFlag, flag)
}
