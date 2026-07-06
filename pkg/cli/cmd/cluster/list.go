package cluster

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/k8s"
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
  PROVIDER   DISTRIBUTION   CLUSTER       STATUS
  docker     Vanilla        dev-cluster   Running
  docker     K3s            test-cluster  Stopped
  hetzner    Talos          prod-cluster  Unknown

The STATUS column reports the cluster's run-state: "Running" when its nodes are
up, "Stopped" when they exist but are not running (e.g. a stopped Docker
cluster), and "Unknown" for providers that cannot report it (cloud providers).
Kubeconfig contexts KSail did not provision are also listed with STATUS
"Unmanaged" (blank PROVIDER/DISTRIBUTION) so they are visible on the CLI just as
in the web UI; KSail-only operations (delete/stop/update) do not act on them.

When any cluster has a TTL set, a TTL column is appended:
  PROVIDER   DISTRIBUTION   CLUSTER       STATUS    TTL
  docker     K3s            dev-cluster   Running   2h 30m

The PROVIDER and CLUSTER values from the output can be used directly
with other cluster commands:
  ksail cluster delete --name <cluster> --provider <provider>
  ksail cluster stop --name <cluster> --provider <provider>

Use --output json for machine-readable output. The JSON is an array of objects:
  [
    {"name": "dev", "provider": "docker", "distribution": "Vanilla", "status": "Running", "ttl": "2h 30m"}
  ]
The "status" field is "Running", "Stopped", "Unknown", or "Unmanaged" (see the STATUS column).
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
			"(array of {name, provider, distribution, status, ttl}).")

	return cmd
}

// ListDeps captures dependencies needed for the list command logic.
type ListDeps struct {
	// DistributionFactoryCreator is an optional function that creates factories for distributions.
	// If nil, real factories with empty configs are used. Primarily for testing: it is routed into
	// the shared clusterdiscovery.Discoverer as the Docker provider's factory.
	DistributionFactoryCreator func(v1alpha1.Distribution) clusterprovisioner.Factory

	// DockerStatusFunc optionally reports a Docker cluster's run-state, routed into the discoverer's
	// DockerStatus seam. If nil, discovery probes the real Docker daemon. Primarily for testing, so
	// the STATUS column is deterministic without a live Docker daemon.
	DockerStatusFunc func(
		ctx context.Context,
		distribution v1alpha1.Distribution,
		name string,
	) clusterdiscovery.RunState

	// KubeconfigPathFunc optionally resolves the kubeconfig path scanned for unmanaged
	// (kubeconfig-only) clusters. If nil, the user's default kubeconfig is used. Primarily for
	// testing, so unmanaged discovery reads a temp kubeconfig instead of the real one.
	KubeconfigPathFunc func() string
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

	clusters, failures := newDiscoverer(deps).Discover(cmd.Context(), providers)

	for _, failure := range failures {
		_, _ = fmt.Fprintf(
			cmd.ErrOrStderr(),
			"Warning: failed to list %s clusters: %v\n",
			failure.Provider,
			failure.Err,
		)
	}

	// When listing all providers (no --provider filter), also surface kubeconfig contexts ksail did
	// not provision — flagged Unmanaged — so an unmanaged cluster visible in the web UI is visible on
	// the CLI too (ksail#5654 surface parity). A provider filter narrows to that provider's managed
	// clusters, so provider-less unmanaged clusters are omitted there.
	if providerFilter == "" {
		clusters = append(clusters, discoverUnmanaged(deps, clusters)...)
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
			RunState:     cluster.RunState,
		})
	}

	if getOutputFormat(cmd) == outputFormatJSON {
		return emitListJSON(cmd.OutOrStdout(), providers, allResults)
	}

	displayListResults(cmd.OutOrStdout(), providers, allResults)

	return nil
}

// newDiscoverer builds the shared clusterdiscovery.Discoverer for the list command, routing the
// optional test seams (Docker factory + run-state probe) from deps. With no seams it queries real
// providers and probes the real Docker daemon for run-state.
func newDiscoverer(deps ListDeps) *clusterdiscovery.Discoverer {
	discoverer := &clusterdiscovery.Discoverer{}
	if deps.DistributionFactoryCreator != nil {
		discoverer.DockerFactory = func(
			distribution v1alpha1.Distribution,
		) (clusterprovisioner.Factory, error) {
			return deps.DistributionFactoryCreator(distribution), nil
		}
	}

	if deps.DockerStatusFunc != nil {
		discoverer.DockerStatus = deps.DockerStatusFunc
	}

	return discoverer
}

// discoverUnmanaged returns the kubeconfig-only (unmanaged) clusters not already among the discovered
// set, keying the managed set by the discovered clusters' names so DiscoverUnmanaged can dedup
// contexts against them. The kubeconfig path comes from the deps seam (the user's default when unset).
func discoverUnmanaged(
	deps ListDeps,
	discovered []clusterdiscovery.Cluster,
) []clusterdiscovery.Cluster {
	managed := make(map[string]struct{}, len(discovered))
	for _, cluster := range discovered {
		managed[cluster.Name] = struct{}{}
	}

	kubeconfigPath := k8s.DefaultKubeconfigPath
	if deps.KubeconfigPathFunc != nil {
		kubeconfigPath = deps.KubeconfigPathFunc
	}

	return clusterdiscovery.DiscoverUnmanaged(kubeconfigPath(), managed)
}

// resolveProviders returns the list of providers to query based on the filter.
func resolveProviders(filter v1alpha1.Provider) []v1alpha1.Provider {
	if filter == "" {
		return allProviders()
	}

	return []v1alpha1.Provider{filter}
}

// displayListResults outputs the cluster list as an aligned table.
// Columns: PROVIDER, DISTRIBUTION, CLUSTER, STATUS, and optionally TTL (when any cluster has one).
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

	printTable(writer, tableHeaders(results), buildTableRows(providers, results))
}

// buildTableRows converts listResults into ordered table rows following provider order. Each row is
// the ordered column values matching the header set built by tableHeaders for the same results.
func buildTableRows(providers []v1alpha1.Provider, results []listResult) [][]string {
	hasTTL := anyTTL(results)

	var rows [][]string

	for _, prov := range providers {
		for _, result := range results {
			if result.Provider != prov {
				continue
			}

			row := []string{
				strings.ToLower(string(result.Provider)),
				string(result.Distribution),
				result.ClusterName,
				statusLabel(result.RunState),
			}
			if hasTTL {
				row = append(row, formatTTLValue(result.TTL))
			}

			rows = append(rows, row)
		}
	}

	// Unmanaged (kubeconfig-only) clusters have no provider, so the provider loop above skips them —
	// append them last with blank PROVIDER/DISTRIBUTION and STATUS=Unmanaged.
	for _, result := range unmanagedResults(results) {
		row := []string{"", "", result.ClusterName, statusLabel(result.RunState)}
		if hasTTL {
			row = append(row, formatTTLValue(result.TTL))
		}

		rows = append(rows, row)
	}

	return rows
}

// unmanagedResults returns the unmanaged (kubeconfig-only) clusters — the ones the provider-ordered
// loops skip because they have no provider. Both the table and JSON builders append these last, so
// the selection lives in one place and the two output paths cannot drift.
func unmanagedResults(results []listResult) []listResult {
	unmanaged := make([]listResult, 0)

	for _, result := range results {
		if result.RunState == clusterdiscovery.RunStateUnmanaged {
			unmanaged = append(unmanaged, result)
		}
	}

	return unmanaged
}

// tableHeaders returns the column headers for the results: the fixed PROVIDER/DISTRIBUTION/CLUSTER/
// STATUS columns plus a trailing TTL column only when some cluster has a TTL (matching buildTableRows).
func tableHeaders(results []listResult) []string {
	headers := []string{"PROVIDER", "DISTRIBUTION", "CLUSTER", "STATUS"}
	if anyTTL(results) {
		headers = append(headers, "TTL")
	}

	return headers
}

// anyTTL reports whether any result has a non-empty TTL display value, gating the TTL column.
func anyTTL(results []listResult) bool {
	for _, result := range results {
		if formatTTLValue(result.TTL) != "" {
			return true
		}
	}

	return false
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

// printTable writes an aligned table with the given header and data rows. Column widths size to the
// widest cell (header or value); the final column is not padded so trailing whitespace is avoided.
func printTable(writer io.Writer, headers []string, rows [][]string) {
	widths := columnWidths(headers, rows)

	printTableLine(writer, headers, widths)

	for _, row := range rows {
		printTableLine(writer, row, widths)
	}
}

// columnWidths computes each column's display width as the widest of its header and any cell.
func columnWidths(headers []string, rows [][]string) []int {
	widths := make([]int, len(headers))
	for col, header := range headers {
		widths[col] = len(header)
	}

	for _, row := range rows {
		for col, cell := range row {
			if col < len(widths) && len(cell) > widths[col] {
				widths[col] = len(cell)
			}
		}
	}

	return widths
}

// printTableLine writes one row (header or data), left-padding every column except the last to its
// width plus the inter-column gap so columns align without trailing whitespace.
func printTableLine(writer io.Writer, cells []string, widths []int) {
	last := len(cells) - 1
	for col, cell := range cells {
		if col == last {
			_, _ = fmt.Fprintln(writer, cell)

			continue
		}

		_, _ = fmt.Fprintf(writer, "%-*s", widths[col]+tableColumnGap, cell)
	}
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
	// RunState is the cluster's coarse running/stopped run-state as reported by discovery (Docker
	// only). It drives the STATUS column and the JSON "status" field. RunStateUnknown for providers
	// that cannot report it (cloud providers today).
	RunState clusterdiscovery.RunState
}

// statusLabel maps a discovered run-state to the human STATUS column / JSON "status" value: "Running"
// for a running cluster, "Stopped" for a stopped one, and "Unknown" when the provider cannot report
// run-state (cloud providers today). It is the single source of the status vocabulary the CLI emits,
// kept aligned with the v1alpha1.ClusterPhase the web UI surfaces (Ready≈Running, Stopped).
func statusLabel(runState clusterdiscovery.RunState) string {
	switch runState {
	case clusterdiscovery.RunStateRunning:
		return "Running"
	case clusterdiscovery.RunStateStopped:
		return "Stopped"
	case clusterdiscovery.RunStateUnmanaged:
		return "Unmanaged"
	case clusterdiscovery.RunStateUnknown:
		return "Unknown"
	default:
		return "Unknown"
	}
}

// tableColumnGap is the minimum gap between columns in table output.
const tableColumnGap = 3
