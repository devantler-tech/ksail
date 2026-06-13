package cluster

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/notify"
	"github.com/devantler-tech/ksail/v7/pkg/svc/clusterdiscovery"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/svc/state"
	"github.com/spf13/cobra"
)

const listLongDesc = `List all Kubernetes clusters managed by KSail.

By default, lists clusters from all distributions across all providers.
Use --provider to filter results to a specific provider.

Output Format:
  PROVIDER   DISTRIBUTION   CLUSTER
  docker     Vanilla        dev-cluster
  docker     K3s            test-cluster
  hetzner    Talos          prod-cluster

When any cluster has a TTL set, a TTL column is included:
  PROVIDER   DISTRIBUTION   CLUSTER       TTL
  docker     K3s            dev-cluster   2h 30m

The PROVIDER and CLUSTER values from the output can be used directly
with other cluster commands:
  ksail cluster delete --name <cluster> --provider <provider>
  ksail cluster stop --name <cluster> --provider <provider>

Use --output json for machine-readable output. The JSON is an array of objects:
  [
    {"name": "dev", "provider": "docker", "distribution": "Vanilla", "ttl": "2h 30m"}
  ]
The "ttl" field is null when no TTL is set and "EXPIRED" once the TTL has elapsed.

Examples:
  # List all clusters
  ksail cluster list

  # List only Docker-based clusters
  ksail cluster list --provider Docker

  # List only Hetzner clusters
  ksail cluster list --provider Hetzner

  # List only Omni clusters
  ksail cluster list --provider Omni

  # Machine-readable JSON (name/provider/distribution/ttl)
  ksail cluster list --output json`

// NewListCmd creates the list command for clusters.
func NewListCmd() *cobra.Command {
	var providerFilter v1alpha1.Provider

	cmd := &cobra.Command{
		Use:          "list",
		Short:        "List clusters",
		Long:         listLongDesc,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			err := validateOutputFormat(cmd)
			if err != nil {
				return err
			}

			return HandleListRunE(cmd, providerFilter, ListDeps{})
		},
	}

	// Add --provider flag as optional filter (no default - lists all by default)
	cmd.Flags().VarP(
		&providerFilter,
		"provider", "p",
		fmt.Sprintf("Filter by provider (%s). If not specified, lists all providers.",
			strings.Join(providerFilter.ValidValues(), ", ")),
	)

	cmd.Flags().String("output", outputFormatText,
		"Output format: text or json. Use json for machine-readable structured output "+
			"(array of {name, provider, distribution, ttl}).")

	return cmd
}

// ListDeps captures dependencies needed for the list command logic.
type ListDeps struct {
	// DistributionFactoryCreator is an optional function that creates factories for distributions.
	// If nil, real factories with empty configs are used. Primarily for testing: it is routed into
	// the shared clusterdiscovery.Discoverer as the Docker provider's factory.
	DistributionFactoryCreator func(v1alpha1.Distribution) clusterprovisioner.Factory
}

// HandleListRunE handles the list command. It delegates cluster enumeration to the shared
// clusterdiscovery service (the same one the local web UI uses) and formats the results as a table.
// Exported for testing purposes.
func HandleListRunE(
	cmd *cobra.Command,
	providerFilter v1alpha1.Provider,
	deps ListDeps,
) error {
	providers := resolveProviders(providerFilter)

	discoverer := &clusterdiscovery.Discoverer{}
	if deps.DistributionFactoryCreator != nil {
		discoverer.DockerFactory = func(
			distribution v1alpha1.Distribution,
		) (clusterprovisioner.Factory, error) {
			return deps.DistributionFactoryCreator(distribution), nil
		}
	}

	clusters, failures := discoverer.Discover(cmd.Context(), providers)

	for _, failure := range failures {
		_, _ = fmt.Fprintf(
			cmd.ErrOrStderr(),
			"Warning: failed to list %s clusters: %v\n",
			failure.Provider,
			failure.Err,
		)
	}

	allResults := make([]listResult, 0, len(clusters))

	for _, cluster := range clusters {
		ttlInfo, ttlErr := state.LoadClusterTTL(cluster.Name)
		if ttlErr != nil && !errors.Is(ttlErr, state.ErrTTLNotSet) {
			notify.Warningf(
				cmd.ErrOrStderr(),
				"failed to load TTL for cluster %q: %v",
				cluster.Name,
				ttlErr,
			)
		}

		var ttl *state.TTLInfo
		if ttlErr == nil {
			ttl = ttlInfo
		}

		allResults = append(allResults, listResult{
			Provider:     cluster.Provider,
			Distribution: cluster.Distribution,
			ClusterName:  cluster.Name,
			TTL:          ttl,
		})
	}

	if getOutputFormat(cmd) == outputFormatJSON {
		return emitListJSON(cmd.OutOrStdout(), providers, allResults)
	}

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

// tableRow holds pre-formatted strings for a single row in the cluster list table.
type tableRow struct {
	provider     string
	distribution string
	cluster      string
	ttl          string
}

// displayListResults outputs the cluster list as an aligned table.
// Columns: PROVIDER, DISTRIBUTION, CLUSTER, and optionally TTL (when any cluster has one).
// If no clusters exist, displays "No clusters found.".
func displayListResults(
	writer io.Writer,
	providers []v1alpha1.Provider,
	results []listResult,
) {
	if len(results) == 0 {
		_, _ = fmt.Fprintln(writer, "No clusters found.")

		return
	}

	rows, hasTTL := buildTableRows(providers, results)
	printTable(writer, rows, hasTTL)
}

// buildTableRows converts listResults into ordered tableRows following provider order.
// Returns the rows and whether any row has a TTL value.
func buildTableRows(providers []v1alpha1.Provider, results []listResult) ([]tableRow, bool) {
	hasTTL := false

	var rows []tableRow

	for _, prov := range providers {
		for _, result := range results {
			if result.Provider != prov {
				continue
			}

			ttlStr := formatTTLValue(result.TTL)
			if ttlStr != "" {
				hasTTL = true
			}

			rows = append(rows, tableRow{
				provider:     strings.ToLower(string(result.Provider)),
				distribution: string(result.Distribution),
				cluster:      result.ClusterName,
				ttl:          ttlStr,
			})
		}
	}

	return rows, hasTTL
}

// formatTTLValue returns the human-readable TTL string for display, or "" if no TTL is set.
func formatTTLValue(ttl *state.TTLInfo) string {
	if ttl == nil {
		return ""
	}

	remaining := ttl.Remaining()
	if remaining <= 0 {
		return "EXPIRED"
	}

	return formatRemainingDuration(remaining)
}

// printTable writes an aligned table of cluster rows to the writer.
func printTable(writer io.Writer, rows []tableRow, hasTTL bool) {
	provW := len("PROVIDER")
	distW := len("DISTRIBUTION")
	clusterW := len("CLUSTER")

	for _, row := range rows {
		if len(row.provider) > provW {
			provW = len(row.provider)
		}

		if len(row.distribution) > distW {
			distW = len(row.distribution)
		}

		if len(row.cluster) > clusterW {
			clusterW = len(row.cluster)
		}
	}

	if hasTTL {
		_, _ = fmt.Fprintf(
			writer, "%-*s%-*s%-*s%s\n",
			provW+tableColumnGap, "PROVIDER",
			distW+tableColumnGap, "DISTRIBUTION",
			clusterW+tableColumnGap, "CLUSTER",
			"TTL",
		)
	} else {
		_, _ = fmt.Fprintf(
			writer, "%-*s%-*s%s\n",
			provW+tableColumnGap, "PROVIDER",
			distW+tableColumnGap, "DISTRIBUTION",
			"CLUSTER",
		)
	}

	for _, row := range rows {
		printTableRow(writer, row, provW, distW, clusterW, hasTTL)
	}
}

// printTableRow writes a single data row. When the table has a TTL column,
// the cluster field is padded for alignment even on rows without a TTL value.
func printTableRow(writer io.Writer, row tableRow, provW, distW, clusterW int, hasTTLColumn bool) {
	if hasTTLColumn {
		_, _ = fmt.Fprintf(
			writer, "%-*s%-*s%-*s%s\n",
			provW+tableColumnGap, row.provider,
			distW+tableColumnGap, row.distribution,
			clusterW+tableColumnGap, row.cluster,
			row.ttl,
		)

		return
	}

	_, _ = fmt.Fprintf(
		writer, "%-*s%-*s%s\n",
		provW+tableColumnGap, row.provider,
		distW+tableColumnGap, row.distribution,
		row.cluster,
	)
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
