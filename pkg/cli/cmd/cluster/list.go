package cluster

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	runtime "github.com/devantler-tech/ksail/v5/pkg/di"
	talosconfigmanager "github.com/devantler-tech/ksail/v5/pkg/io/config-manager/talos"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provider/hetzner"
	clusterprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster"
	clustererrors "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/errors"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	k3dv1alpha5 "github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
	"github.com/spf13/cobra"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

// ErrUnsupportedProvider re-exports the shared error for backward compatibility.
var ErrUnsupportedProvider = clustererrors.ErrUnsupportedProvider

// AllDistributions returns all supported distributions.
func AllDistributions() []v1alpha1.Distribution {
	return []v1alpha1.Distribution{
		v1alpha1.DistributionVanilla,
		v1alpha1.DistributionK3s,
		v1alpha1.DistributionTalos,
	}
}

// AllProviders returns all supported providers.
func AllProviders() []v1alpha1.Provider {
	return []v1alpha1.Provider{
		v1alpha1.ProviderDocker,
		v1alpha1.ProviderHetzner,
	}
}

// clusterResult holds a cluster name with its provider for display purposes.
type clusterResult struct {
	Provider    v1alpha1.Provider
	ClusterName string
}

const listLongDesc = `List all Kubernetes clusters managed by KSail.

By default, lists clusters from all distributions across all providers.
Use --provider to filter results to a specific provider.

Examples:
  # List all clusters
  ksail cluster list

  # List only Docker-based clusters
  ksail cluster list --provider Docker

  # List only Hetzner clusters
  ksail cluster list --provider Hetzner`

// NewListCmd creates the list command for clusters.
func NewListCmd(runtimeContainer *runtime.Runtime) *cobra.Command {
	var providerFilter v1alpha1.Provider

	cmd := &cobra.Command{
		Use:          "list",
		Short:        "List clusters",
		Long:         listLongDesc,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runtimeContainer.Invoke(func(_ runtime.Injector) error {
				deps := ListDeps{}

				return HandleListRunE(cmd, providerFilter, deps)
			})
		},
	}

	// Add --provider flag as optional filter (no default - lists all by default)
	cmd.Flags().VarP(
		&providerFilter,
		"provider", "p",
		fmt.Sprintf("Filter by provider (%s). If not specified, lists all providers.",
			strings.Join(providerFilter.ValidValues(), ", ")),
	)

	return cmd
}

// ListDeps captures dependencies needed for the list command logic.
type ListDeps struct {
	// DistributionFactoryCreator is an optional function that creates factories for distributions.
	// If nil, real factories with empty configs are used.
	// This is primarily for testing purposes.
	DistributionFactoryCreator func(v1alpha1.Distribution) clusterprovisioner.Factory

	// HetznerProvider is an optional Hetzner provider for listing Hetzner clusters.
	// If nil, a real provider will be created if HCLOUD_TOKEN is set.
	HetznerProvider *hetzner.Provider
}

// HandleListRunE handles the list command.
// Exported for testing purposes.
func HandleListRunE(
	cmd *cobra.Command,
	providerFilter v1alpha1.Provider,
	deps ListDeps,
) error {
	// Determine which providers to query
	providers := resolveProviders(providerFilter)

	// Collect clusters from all providers
	var allResults []clusterResult

	for _, prov := range providers {
		clusters, err := getProviderClusters(cmd.Context(), deps, prov)
		if err != nil {
			// Log warning but continue with other providers
			_, _ = fmt.Fprintf(
				cmd.ErrOrStderr(),
				"Warning: failed to list %s clusters: %v\n",
				prov,
				err,
			)

			continue
		}

		for _, cluster := range clusters {
			allResults = append(allResults, clusterResult{
				Provider:    prov,
				ClusterName: cluster,
			})
		}
	}

	// Display results
	displayListResults(cmd.OutOrStdout(), providers, allResults)

	return nil
}

// resolveProviders returns the list of providers to query based on the filter.
func resolveProviders(filter v1alpha1.Provider) []v1alpha1.Provider {
	if filter == "" {
		return AllProviders()
	}

	return []v1alpha1.Provider{filter}
}

// getProviderClusters returns all clusters for a given provider.
func getProviderClusters(
	ctx context.Context,
	deps ListDeps,
	provider v1alpha1.Provider,
) ([]string, error) {
	switch provider {
	case v1alpha1.ProviderDocker:
		return getDockerClusters(ctx, deps)
	case v1alpha1.ProviderHetzner:
		return getHetznerClusters(ctx, deps)
	default:
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedProvider, provider)
	}
}

// getDockerClusters returns all Docker-based clusters across all distributions.
func getDockerClusters(ctx context.Context, deps ListDeps) ([]string, error) {
	var allClusters []string

	for _, dist := range AllDistributions() {
		clusters, err := getDistributionClusters(ctx, deps, dist)
		if err != nil {
			// Log and continue - don't fail on one distribution
			continue
		}

		allClusters = append(allClusters, clusters...)
	}

	return allClusters, nil
}

// getHetznerClusters returns all Hetzner-based clusters.
func getHetznerClusters(ctx context.Context, deps ListDeps) ([]string, error) {
	// Use injected provider if available (for testing)
	if deps.HetznerProvider != nil {
		clusters, err := deps.HetznerProvider.ListAllClusters(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to list Hetzner clusters: %w", err)
		}

		return clusters, nil
	}

	// Check for HCLOUD_TOKEN
	token := os.Getenv("HCLOUD_TOKEN")
	if token == "" {
		// No token, skip Hetzner silently
		return nil, nil
	}

	client := hcloud.NewClient(hcloud.WithToken(token))
	provider := hetzner.NewProvider(client)

	clusters, err := provider.ListAllClusters(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list Hetzner clusters: %w", err)
	}

	return clusters, nil
}

func getDistributionClusters(
	ctx context.Context,
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

	provisioner, _, err := factory.Create(ctx, clusterCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create provisioner for %s: %w", distribution, err)
	}

	clusters, err := provisioner.List(ctx)
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

// displayListResults outputs the cluster list grouped by provider.
// Only providers with clusters are shown, formatted as "provider: cluster1, cluster2".
// If no clusters exist, displays "No clusters found.".
func displayListResults(
	writer io.Writer,
	providers []v1alpha1.Provider,
	results []clusterResult,
) {
	if len(results) == 0 {
		_, _ = fmt.Fprintln(writer, "No clusters found.")

		return
	}

	// Group clusters by provider
	providerClusters := make(map[v1alpha1.Provider][]string)
	for _, r := range results {
		providerClusters[r.Provider] = append(providerClusters[r.Provider], r.ClusterName)
	}

	// Output in provider order for consistent output
	for _, prov := range providers {
		clusters, exists := providerClusters[prov]
		if !exists || len(clusters) == 0 {
			continue
		}

		_, _ = fmt.Fprintf(
			writer,
			"%s: %s\n",
			strings.ToLower(string(prov)),
			strings.Join(clusters, ", "),
		)
	}
}
