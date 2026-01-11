package cluster

import (
	"fmt"
	"io"
	"strings"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	runtime "github.com/devantler-tech/ksail/v5/pkg/di"
	talosconfigmanager "github.com/devantler-tech/ksail/v5/pkg/io/config-manager/talos"
	clusterprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster"
	k3dv1alpha5 "github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
	"github.com/spf13/cobra"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

// AllDistributions returns all supported distributions.
func AllDistributions() []v1alpha1.Distribution {
	return []v1alpha1.Distribution{
		v1alpha1.DistributionVanilla,
		v1alpha1.DistributionK3s,
		v1alpha1.DistributionTalos,
	}
}

// NewListCmd creates the list command for clusters.
func NewListCmd(runtimeContainer *runtime.Runtime) *cobra.Command {
	var distributionFilter string

	cmd := &cobra.Command{
		Use:          "list",
		Short:        "List clusters",
		Long:         `List all Kubernetes clusters managed by KSail.`,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runtimeContainer.Invoke(func(_ runtime.Injector) error {
				deps := ListDeps{}

				return HandleListRunE(cmd, distributionFilter, deps)
			})
		},
	}

	// Add --distribution flag as optional filter (no default - lists all by default)
	cmd.Flags().StringVarP(
		&distributionFilter,
		"distribution", "d", "",
		"Filter by distribution (Vanilla, K3s, Talos). If not specified, lists all distributions.",
	)

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
	distributionFilter string,
	deps ListDeps,
) error {
	// Determine which distributions to list
	distributions, err := resolveDistributions(distributionFilter)
	if err != nil {
		return err
	}

	// Collect clusters from all distributions
	results := make(map[v1alpha1.Distribution][]string)

	for _, dist := range distributions {
		clusters, listErr := getDistributionClusters(cmd, deps, dist)
		if listErr != nil {
			return listErr
		}

		if len(clusters) > 0 {
			results[dist] = clusters
		}
	}

	// Display results
	displayResults(cmd.OutOrStdout(), distributions, results)

	return nil
}

// resolveDistributions returns the list of distributions to query based on the filter.
func resolveDistributions(filter string) ([]v1alpha1.Distribution, error) {
	if filter == "" {
		return AllDistributions(), nil
	}

	// Parse and validate the distribution filter
	var dist v1alpha1.Distribution

	err := dist.Set(filter)
	if err != nil {
		return nil, fmt.Errorf("invalid distribution filter: %w", err)
	}

	return []v1alpha1.Distribution{dist}, nil
}

func getDistributionClusters(
	cmd *cobra.Command,
	deps ListDeps,
	distribution v1alpha1.Distribution,
) ([]string, error) {
	// Create a minimal cluster config for the factory
	clusterCfg := &v1alpha1.Cluster{
		Spec: v1alpha1.Spec{
			Cluster: v1alpha1.ClusterSpec{
				Distribution: distribution,
			},
		},
	}

	// Use custom factory creator if provided (for testing), otherwise create real factory.
	var factory clusterprovisioner.Factory
	if deps.DistributionFactoryCreator != nil {
		factory = deps.DistributionFactoryCreator(distribution)
	} else {
		// Create a factory with an empty config for the distribution.
		// For list operations, we only need the provisioner type, not specific config data.
		factory = clusterprovisioner.DefaultFactory{
			DistributionConfig: createEmptyDistributionConfig(distribution),
		}
	}

	provisioner, _, err := factory.Create(cmd.Context(), clusterCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create provisioner for %s: %w", distribution, err)
	}

	clusters, err := provisioner.List(cmd.Context())
	if err != nil {
		return nil, fmt.Errorf("failed to list %s clusters: %w", distribution, err)
	}

	return clusters, nil
}

// createEmptyDistributionConfig creates an empty distribution config for the given distribution.
// This is used for list operations where we only need the provisioner type, not specific config data.
func createEmptyDistributionConfig(
	distribution v1alpha1.Distribution,
) *clusterprovisioner.DistributionConfig {
	switch distribution {
	case v1alpha1.DistributionVanilla:
		return &clusterprovisioner.DistributionConfig{
			Kind: &v1alpha4.Cluster{},
		}
	case v1alpha1.DistributionK3s:
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

// displayResults outputs the cluster list in a simplified format.
// Only distributions with clusters are shown, formatted as "distribution: cluster1, cluster2".
// If no clusters exist across all distributions, displays "No clusters found.".
func displayResults(
	writer io.Writer,
	distributions []v1alpha1.Distribution,
	results map[v1alpha1.Distribution][]string,
) {
	if len(results) == 0 {
		_, _ = fmt.Fprintln(writer, "No clusters found.")

		return
	}

	// Output in distribution order for consistent output
	for _, dist := range distributions {
		clusters, exists := results[dist]
		if !exists || len(clusters) == 0 {
			continue
		}

		_, _ = fmt.Fprintf(
			writer,
			"%s: %s\n",
			strings.ToLower(string(dist)),
			strings.Join(clusters, ", "),
		)
	}
}
