package cluster

import (
	"errors"
	"fmt"
	"strings"

	"github.com/devantler-tech/ksail-go/pkg/apis/cluster/v1alpha1"
	runtime "github.com/devantler-tech/ksail-go/pkg/di"
	ksailconfigmanager "github.com/devantler-tech/ksail-go/pkg/io/config-manager/ksail"
	clusterprovisioner "github.com/devantler-tech/ksail-go/pkg/svc/provisioner/cluster"
	"github.com/devantler-tech/ksail-go/pkg/ui/notify"
	"github.com/samber/do/v2"
	"github.com/spf13/cobra"
)

const allFlag = "all"

var errDistributionFactoryUnset = errors.New("distribution factory dependency is not configured")

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
		return runtimeContainer.Invoke(func(injector runtime.Injector) error {
			factory, err := do.Invoke[clusterprovisioner.Factory](injector)
			if err != nil {
				return fmt.Errorf("resolve provisioner factory dependency: %w", err)
			}

			deps := ListDeps{
				Factory:             factory,
				DistributionFactory: clusterprovisioner.DefaultFactory{},
			}

			return HandleListRunE(cmd, cfgManager, deps)
		})
	}

	return cmd
}

// ListDeps captures dependencies needed for the list command logic.
type ListDeps struct {
	Factory             clusterprovisioner.Factory
	DistributionFactory clusterprovisioner.Factory
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
	provisioner, _, err := deps.Factory.Create(cmd.Context(), clusterCfg)
	if err != nil {
		return fmt.Errorf("failed to resolve cluster provisioner: %w", err)
	}

	clusters, err := provisioner.List(cmd.Context())
	if err != nil {
		return fmt.Errorf("failed to list clusters: %w", err)
	}

	displayClusterList(clusterCfg.Spec.Distribution, clusters, cmd, includeDistribution)

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
	} {
		if distribution == clusterCfg.Spec.Distribution {
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

	distributionFactory := deps.DistributionFactory
	if distributionFactory == nil {
		return fmt.Errorf(
			"distribution factory dependency is not configured: %w",
			errDistributionFactoryUnset,
		)
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
	clone.Spec.Distribution = distribution

	if distribution != original.Spec.Distribution {
		clone.Spec.DistributionConfig = defaultDistributionConfigPath(distribution)
	}

	return &clone
}

func defaultDistributionConfigPath(distribution v1alpha1.Distribution) string {
	switch distribution {
	case v1alpha1.DistributionKind:
		return "kind.yaml"
	case v1alpha1.DistributionK3d:
		return "k3d.yaml"
	default:
		return "kind.yaml"
	}
}

func displayClusterList(
	distribution v1alpha1.Distribution,
	clusters []string,
	cmd *cobra.Command,
	includeDistribution bool,
) {
	if len(clusters) == 0 {
		notify.WriteMessage(notify.Message{
			Type:    notify.ActivityType,
			Content: "no clusters found",
			Writer:  cmd.OutOrStdout(),
		})
	} else {
		writer := cmd.OutOrStdout()

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
}

func bindAllFlag(cmd *cobra.Command, cfgManager *ksailconfigmanager.ConfigManager) {
	cmd.Flags().
		BoolP(allFlag, "a", false, "List all clusters, including those not defined in the configuration")
	flag := cmd.Flags().Lookup(allFlag)
	_ = cfgManager.Viper.BindPFlag(allFlag, flag)
}
