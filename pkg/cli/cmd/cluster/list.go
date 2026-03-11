package cluster

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/di"
	talosconfigmanager "github.com/devantler-tech/ksail/v5/pkg/fsutil/configmanager/talos"
	"github.com/devantler-tech/ksail/v5/pkg/notify"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provider/hetzner"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provider/omni"
	clusterprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/clustererr"
	"github.com/devantler-tech/ksail/v5/pkg/svc/state"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	k3dv1alpha5 "github.com/k3d-io/k3d/v5/pkg/config/v1alpha5"
	omniclient "github.com/siderolabs/omni/client/pkg/client"
	"github.com/spf13/cobra"
	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
)

// ErrUnsupportedProvider re-exports the shared error for backward compatibility.
var ErrUnsupportedProvider = clustererr.ErrUnsupportedProvider

// allDistributions returns all supported distributions.
func allDistributions() []v1alpha1.Distribution {
	return []v1alpha1.Distribution{
		v1alpha1.DistributionVanilla,
		v1alpha1.DistributionK3s,
		v1alpha1.DistributionTalos,
		v1alpha1.DistributionVCluster,
	}
}

// allProviders returns all supported providers.
func allProviders() []v1alpha1.Provider {
	return []v1alpha1.Provider{
		v1alpha1.ProviderDocker,
		v1alpha1.ProviderHetzner,
		v1alpha1.ProviderOmni,
	}
}

// listResult holds a cluster name with its provider for display purposes.
type listResult struct {
	Provider    v1alpha1.Provider
	ClusterName string
	TTL         *state.TTLInfo // nil if no TTL has been set for this cluster
}

// ttlIndent is the indentation prefix for TTL annotation lines in list output.
const ttlIndent = "  "

const listLongDesc = `List all Kubernetes clusters managed by KSail.

By default, lists clusters from all distributions across all providers.
Use --provider to filter results to a specific provider.

Output Format:
  <provider>: <cluster_name>[, <cluster_name>...]

Each line groups clusters by provider. For example:
  docker: dev-cluster, test-cluster

The provider name (docker or hetzner) and each cluster name from the
output can be used directly with other cluster commands:
  ksail cluster delete --name <cluster_name> --provider <provider>
  ksail cluster stop --name <cluster_name> --provider <provider>

Examples:
  # List all clusters
  ksail cluster list

  # List only Docker-based clusters
  ksail cluster list --provider Docker

  # List only Hetzner clusters
  ksail cluster list --provider Hetzner

  # List only Omni clusters
  ksail cluster list --provider Omni`

// NewListCmd creates the list command for clusters.
func NewListCmd(runtimeContainer *di.Runtime) *cobra.Command {
	var providerFilter v1alpha1.Provider

	cmd := &cobra.Command{
		Use:          "list",
		Short:        "List clusters",
		Long:         listLongDesc,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runtimeContainer.Invoke(func(_ di.Injector) error {
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

	// OmniProvider is an optional Omni provider for listing Omni clusters.
	// If nil, a real provider will be created if OMNI_SERVICE_ACCOUNT_KEY is set.
	OmniProvider *omni.Provider
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
	var allResults []listResult

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
			ttlInfo, ttlErr := state.LoadClusterTTL(cluster)
			if ttlErr != nil && !errors.Is(ttlErr, state.ErrTTLNotSet) {
				notify.Warningf(
					cmd.ErrOrStderr(),
					"failed to load TTL for cluster %q: %v",
					cluster,
					ttlErr,
				)
			}

			var ttl *state.TTLInfo
			if ttlErr == nil {
				ttl = ttlInfo
			}

			allResults = append(allResults, listResult{
				Provider:    prov,
				ClusterName: cluster,
				TTL:         ttl,
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
		return allProviders()
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
	case v1alpha1.ProviderOmni:
		return getOmniClusters(ctx, deps)
	default:
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedProvider, provider)
	}
}

// getDockerClusters returns all Docker-based clusters across all distributions.
func getDockerClusters(ctx context.Context, deps ListDeps) ([]string, error) {
	var allClusters []string

	for _, dist := range allDistributions() {
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

// getOmniClusters returns all Omni-based clusters.
func getOmniClusters(ctx context.Context, deps ListDeps) ([]string, error) {
	// Use injected provider if available (for testing)
	if deps.OmniProvider != nil {
		clusters, err := deps.OmniProvider.ListAllClusters(ctx)
		if err != nil {
			return nil, fmt.Errorf("failed to list Omni clusters: %w", err)
		}

		return clusters, nil
	}

	// Check for OMNI_SERVICE_ACCOUNT_KEY
	serviceAccountKey := os.Getenv("OMNI_SERVICE_ACCOUNT_KEY")
	if serviceAccountKey == "" {
		// No key, skip Omni silently
		return nil, nil
	}

	// Check for OMNI_ENDPOINT
	endpoint := os.Getenv("OMNI_ENDPOINT")
	if endpoint == "" {
		// No endpoint, skip Omni silently
		return nil, nil
	}

	client, err := omniclient.New(
		endpoint,
		omniclient.WithServiceAccount(serviceAccountKey),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create Omni client: %w", err)
	}

	provider := omni.NewProvider(client)

	clusters, err := provider.ListAllClusters(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list Omni clusters: %w", err)
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
	case v1alpha1.DistributionVCluster:
		return &clusterprovisioner.DistributionConfig{
			VCluster: &clusterprovisioner.VClusterConfig{},
		}
	default:
		return &clusterprovisioner.DistributionConfig{
			Kind: &v1alpha4.Cluster{},
		}
	}
}

// displayListResults outputs the cluster list grouped by provider.
// Output is formatted for clarity, especially for AI assistants that need
// to parse the cluster names for subsequent commands.
// Format: "<provider>: cluster1, cluster2" to clearly identify cluster names.
// If no clusters exist, displays "No clusters found.".
// Clusters with a TTL set show remaining time on a separate indented line
// to keep printed cluster identifiers directly copy/paste-able.
func displayListResults(
	writer io.Writer,
	providers []v1alpha1.Provider,
	results []listResult,
) {
	if len(results) == 0 {
		_, _ = fmt.Fprintln(writer, "No clusters found.")

		return
	}

	// Group clusters by provider, preserving TTL info for formatting.
	type clusterEntry struct {
		name string
		ttl  *state.TTLInfo
	}

	providerClusters := make(map[v1alpha1.Provider][]clusterEntry)
	for _, r := range results {
		providerClusters[r.Provider] = append(providerClusters[r.Provider], clusterEntry{
			name: r.ClusterName,
			ttl:  r.TTL,
		})
	}

	// Output in provider order for consistent output.
	// Format explicitly labels cluster names for AI parsing.
	for _, prov := range providers {
		entries, exists := providerClusters[prov]
		if !exists || len(entries) == 0 {
			continue
		}

		clusterNames := make([]string, 0, len(entries))
		for _, e := range entries {
			clusterNames = append(clusterNames, e.name)
		}

		_, _ = fmt.Fprintf(
			writer,
			"%s: %s\n",
			strings.ToLower(string(prov)),
			strings.Join(clusterNames, ", "),
		)

		// Print TTL annotations on separate indented lines.
		for _, e := range entries {
			ttlLabel := formatTTLLabel(e.ttl)
			if ttlLabel != "" {
				_, _ = fmt.Fprintf(writer, "%s%s %s\n", ttlIndent, e.name, ttlLabel)
			}
		}
	}
}

// formatTTLLabel returns a TTL annotation string for a cluster entry.
// Returns "" when no TTL is set, "[TTL: EXPIRED]" when expired,
// or "[TTL: Xh Ym]" with remaining time.
func formatTTLLabel(ttl *state.TTLInfo) string {
	if ttl == nil {
		return ""
	}

	remaining := ttl.Remaining()
	if remaining <= 0 {
		return "[TTL: EXPIRED]"
	}

	return "[TTL: " + formatRemainingDuration(remaining) + "]"
}

// minutesPerHour is the number of minutes in one hour.
const minutesPerHour = 60

// formatRemainingDuration formats a positive duration as a human-readable string.
// Durations are truncated (floored) to whole minutes so the display never overstates
// remaining time. Values under one minute display as "<1m".
func formatRemainingDuration(d time.Duration) string {
	d = d.Truncate(time.Minute)

	hours := int(d.Hours())
	minutes := int(d.Minutes()) % minutesPerHour

	switch {
	case hours > 0 && minutes > 0:
		return fmt.Sprintf("%dh %dm", hours, minutes)
	case hours > 0:
		return fmt.Sprintf("%dh", hours)
	case minutes > 0:
		return fmt.Sprintf("%dm", minutes)
	default:
		return "<1m"
	}
}
