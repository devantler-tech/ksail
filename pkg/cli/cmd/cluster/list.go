package cluster

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/kubeconfig"
	"github.com/devantler-tech/ksail/v7/pkg/cli/setup"
	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	"github.com/devantler-tech/ksail/v7/pkg/notify"
	"github.com/devantler-tech/ksail/v7/pkg/svc/clusterdiscovery"
	"github.com/devantler-tech/ksail/v7/pkg/svc/installer"
	fluxinstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/flux"
	clusterprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
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
func NewListCmd() *cobra.Command {
	var providerFilter v1alpha1.Provider

	cmd := &cobra.Command{
		Use:          "list",
		Short:        "List clusters",
		Long:         listLongDesc,
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
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

// defaultReconcileTimeout is the default timeout for component reconciliation operations.
const defaultReconcileTimeout = 5 * time.Minute

// errMetricsServerDisableUnsupported is returned when attempting to disable metrics-server in-place.
var errMetricsServerDisableUnsupported = errors.New(
	"disabling metrics-server in-place is not yet supported; use 'ksail cluster delete && ksail cluster create'",
)

// componentReconciler applies component-level changes detected by the DiffEngine.
// It maps field names from the diff to installer Install/Uninstall operations.
type componentReconciler struct {
	cmd         *cobra.Command
	clusterCfg  *v1alpha1.Cluster
	clusterName string
	factories   *setup.InstallerFactories
	// autoscalerReconciled tracks whether the cluster autoscaler has already been
	// reconciled during this update pass. Multiple diff fields share the
	// "cluster.autoscaler.node." prefix and map to a single Helm operation;
	// this flag deduplicates the install/upgrade/uninstall call.
	// autoscalerErr preserves the error from the first attempt so that
	// subsequent calls surface the same failure instead of silently succeeding.
	autoscalerReconciled bool
	autoscalerErr        error
}

// newComponentReconciler creates a reconciler for applying component changes.
func newComponentReconciler(
	cmd *cobra.Command,
	clusterCfg *v1alpha1.Cluster,
	clusterName string,
) *componentReconciler {
	return &componentReconciler{
		cmd:         cmd,
		clusterCfg:  clusterCfg,
		clusterName: clusterName,
		factories:   getInstallerFactories(),
	}
}

// reconcileComponents applies in-place component changes from the diff.
// It processes each component change and records results in the provided UpdateResult.
// Returns the number of successfully applied changes and any error from the last failure.
func (r *componentReconciler) reconcileComponents(
	ctx context.Context,
	diff *clusterupdate.UpdateResult,
	result *clusterupdate.UpdateResult,
) error {
	var lastErr error

	for _, change := range diff.InPlaceChanges {
		handler, ok := r.handlerForField(change.Field)
		if !ok {
			continue // Not a component field — handled by provisioner
		}

		err := handler(ctx, change)
		if err != nil {
			result.FailedChanges = append(result.FailedChanges, clusterupdate.Change{
				Field:    change.Field,
				OldValue: change.OldValue,
				NewValue: change.NewValue,
				Category: clusterupdate.ChangeCategoryInPlace,
				Reason:   fmt.Sprintf("failed to reconcile: %v", err),
			})

			lastErr = err

			continue
		}

		result.AppliedChanges = append(result.AppliedChanges, clusterupdate.Change{
			Field:    change.Field,
			OldValue: change.OldValue,
			NewValue: change.NewValue,
			Category: clusterupdate.ChangeCategoryInPlace,
			Reason:   "component reconciled successfully",
		})
	}

	return lastErr
}

// handlerForField returns the reconciliation handler for a given diff field name.
// Returns false if the field is not a component field (e.g., node counts, registry settings).
func (r *componentReconciler) handlerForField(
	field string,
) (func(context.Context, clusterupdate.Change) error, bool) {
	handlers := map[string]func(context.Context, clusterupdate.Change) error{
		"cluster.cni":                               r.reconcileCNI,
		"cluster.csi":                               r.reconcileCSI,
		"cluster.metricsServer":                     r.reconcileMetricsServer,
		"cluster.loadBalancer":                      r.reconcileLoadBalancer,
		"cluster.certManager":                       r.reconcileCertManager,
		"cluster.policyEngine":                      r.reconcilePolicyEngine,
		"cluster.gitOpsEngine":                      r.reconcileGitOpsEngine,
		"cluster.workload.tag":                      r.reconcileWorkloadTag,
		"cluster.workload.flux.distributionVersion": r.reconcileFluxVersion,
	}

	if handler, ok := handlers[field]; ok {
		return handler, true
	}

	// Prefix-based matching for fields with dynamic suffixes.
	// All cluster.autoscaler.node.* fields (enabled, maxNodesTotal, expander,
	// scaleDownUnneededTime, pools[...]) map to a single Helm install/upgrade.
	if strings.HasPrefix(field, "cluster.autoscaler.node.") {
		return r.reconcileClusterAutoscaler, true
	}

	return nil, false
}

// reconcileCNI switches the CNI by installing the new CNI.
// The old CNI is not uninstalled — the new CNI replaces it via Helm upgrade.
func (r *componentReconciler) reconcileCNI(_ context.Context, _ clusterupdate.Change) error {
	_, err := setup.InstallCNI(r.cmd, r.clusterCfg, nil)
	if err != nil {
		return fmt.Errorf("failed to install CNI: %w", err)
	}

	return nil
}

// reconcileCSI installs or uninstalls the CSI driver.
func (r *componentReconciler) reconcileCSI(ctx context.Context, change clusterupdate.Change) error {
	if r.factories.CSI == nil {
		return setup.ErrCSIInstallerFactoryNil
	}

	newValue := v1alpha1.CSI(change.NewValue)
	oldValue := v1alpha1.CSI(change.OldValue)

	// If new value disables CSI, uninstall the old one (only if it was installed)
	if newValue == v1alpha1.CSIDisabled {
		if oldValue == v1alpha1.CSIDisabled || oldValue == "" {
			return nil
		}

		return r.uninstallWithFactory(ctx, r.factories.CSI)
	}

	// Install the new CSI
	err := setup.InstallCSISilent(ctx, r.clusterCfg, r.factories)
	if err != nil {
		return fmt.Errorf("failed to install CSI: %w", err)
	}

	return nil
}

// reconcileMetricsServer installs or uninstalls the metrics server.
func (r *componentReconciler) reconcileMetricsServer(
	ctx context.Context,
	change clusterupdate.Change,
) error {
	newValue := v1alpha1.MetricsServer(change.NewValue)

	if newValue == v1alpha1.MetricsServerDisabled {
		return errMetricsServerDisableUnsupported
	}

	if setup.NeedsMetricsServerInstall(r.clusterCfg) {
		err := setup.InstallMetricsServerSilent(ctx, r.clusterCfg, r.factories)
		if err != nil {
			return fmt.Errorf("failed to install metrics-server: %w", err)
		}
	}

	return nil
}

// reconcileLoadBalancer installs or uninstalls the load balancer.
func (r *componentReconciler) reconcileLoadBalancer(
	ctx context.Context,
	_ clusterupdate.Change,
) error {
	if setup.NeedsLoadBalancerInstall(r.clusterCfg) {
		err := setup.InstallLoadBalancerSilent(ctx, r.clusterCfg, r.factories)
		if err != nil {
			return fmt.Errorf("failed to install load balancer: %w", err)
		}
	}

	return nil
}

// reconcileClusterAutoscaler installs or uninstalls the Cluster Autoscaler.
// Multiple autoscaler diff fields (enabled, maxNodesTotal, pools, …) map to this
// single handler. The autoscalerReconciled guard ensures the Helm operation runs
// at most once per update pass; if it fails, subsequent calls replay the same error
// so the failure is not masked by silent success.
func (r *componentReconciler) reconcileClusterAutoscaler(
	ctx context.Context,
	_ clusterupdate.Change,
) error {
	if r.autoscalerReconciled {
		return r.autoscalerErr
	}

	r.autoscalerReconciled = true

	r.autoscalerErr = r.doReconcileClusterAutoscaler(ctx)

	return r.autoscalerErr
}

// doReconcileClusterAutoscaler performs the actual install/uninstall logic.
func (r *componentReconciler) doReconcileClusterAutoscaler(ctx context.Context) error {
	if setup.NeedsClusterAutoscalerInstall(r.clusterCfg) {
		err := setup.InstallClusterAutoscalerSilent(ctx, r.clusterCfg, r.factories)
		if err != nil {
			return fmt.Errorf("failed to install cluster-autoscaler: %w", err)
		}

		return nil
	}

	// Autoscaler no longer needed — uninstall only on Talos × Hetzner clusters
	// where it could have been previously installed. Treat "release not found"
	// as a successful no-op so the update is idempotent when the autoscaler was
	// never installed (e.g. cluster created before autoscaler support).
	if r.clusterCfg.Spec.Cluster.Distribution != v1alpha1.DistributionTalos ||
		r.clusterCfg.Spec.Cluster.Provider != v1alpha1.ProviderHetzner {
		return nil
	}

	if r.factories.ClusterAutoscaler == nil {
		return setup.ErrClusterAutoscalerInstallerFactoryNil
	}

	err := r.uninstallWithFactory(ctx, r.factories.ClusterAutoscaler)
	if err != nil && errors.Is(err, helm.ErrReleaseNotFound) {
		return nil
	}

	return err
}

// reconcileCertManager installs or uninstalls cert-manager.
func (r *componentReconciler) reconcileCertManager(
	ctx context.Context,
	change clusterupdate.Change,
) error {
	if r.factories.CertManager == nil {
		return setup.ErrCertManagerInstallerFactoryNil
	}

	newValue := v1alpha1.CertManager(change.NewValue)
	oldValue := v1alpha1.CertManager(change.OldValue)

	if newValue == v1alpha1.CertManagerDisabled {
		// If already disabled, nothing to uninstall
		if oldValue == v1alpha1.CertManagerDisabled || oldValue == "" {
			return nil
		}

		return r.uninstallWithFactory(ctx, r.factories.CertManager)
	}

	err := setup.InstallCertManagerSilent(ctx, r.clusterCfg, r.factories)
	if err != nil {
		return fmt.Errorf("failed to install cert-manager: %w", err)
	}

	return nil
}

// reconcilePolicyEngine installs or uninstalls the policy engine.
func (r *componentReconciler) reconcilePolicyEngine(
	ctx context.Context,
	change clusterupdate.Change,
) error {
	newValue := v1alpha1.PolicyEngine(change.NewValue)
	oldValue := v1alpha1.PolicyEngine(change.OldValue)

	if newValue == v1alpha1.PolicyEngineNone || newValue == "" {
		// If already none/disabled, nothing to uninstall
		if oldValue == v1alpha1.PolicyEngineNone || oldValue == "" {
			return nil
		}

		if r.factories.PolicyEngine == nil {
			return setup.ErrPolicyEngineInstallerFactoryNil
		}

		return r.uninstallWithFactory(ctx, r.factories.PolicyEngine)
	}

	if r.factories.PolicyEngine == nil {
		return setup.ErrPolicyEngineInstallerFactoryNil
	}

	err := setup.InstallPolicyEngineSilent(ctx, r.clusterCfg, r.factories)
	if err != nil {
		return fmt.Errorf("failed to install policy engine: %w", err)
	}

	return nil
}

// reconcileGitOpsEngine installs or uninstalls the GitOps engine.
//
//nolint:exhaustive // Only Flux and ArgoCD are installable; None is handled above
func (r *componentReconciler) reconcileGitOpsEngine(
	ctx context.Context,
	change clusterupdate.Change,
) error {
	newValue := v1alpha1.GitOpsEngine(change.NewValue)
	oldValue := v1alpha1.GitOpsEngine(change.OldValue)

	if newValue == v1alpha1.GitOpsEngineNone || newValue == "" {
		// If already none/disabled, nothing to uninstall
		if oldValue == v1alpha1.GitOpsEngineNone || oldValue == "" {
			return nil
		}

		return r.uninstallGitOpsEngine(ctx, change)
	}

	// Install the new GitOps engine
	switch newValue {
	case v1alpha1.GitOpsEngineFlux:
		err := setup.InstallFluxSilent(ctx, r.clusterCfg, r.factories)
		if err != nil {
			return fmt.Errorf("failed to install Flux: %w", err)
		}

		return nil
	case v1alpha1.GitOpsEngineArgoCD:
		err := setup.InstallArgoCDSilent(ctx, r.clusterCfg, r.factories)
		if err != nil {
			return fmt.Errorf("failed to install ArgoCD: %w", err)
		}

		return nil
	default:
		return nil
	}
}

// uninstallGitOpsEngine uninstalls the old GitOps engine.
//
//nolint:exhaustive // Only Flux and ArgoCD can be uninstalled; other values are no-op
func (r *componentReconciler) uninstallGitOpsEngine(
	ctx context.Context,
	change clusterupdate.Change,
) error {
	oldValue := v1alpha1.GitOpsEngine(change.OldValue)

	switch oldValue {
	case v1alpha1.GitOpsEngineFlux:
		helmClient, _, err := r.factories.HelmClientFactory(r.clusterCfg)
		if err != nil {
			return fmt.Errorf("failed to create helm client for Flux uninstall: %w", err)
		}

		fluxInst := r.factories.Flux(
			helmClient,
			defaultReconcileTimeout,
			r.clusterCfg.Spec.Workload.Flux.OperatorVersion,
		)

		err = fluxInst.Uninstall(ctx)
		if err != nil {
			return fmt.Errorf("failed to uninstall Flux: %w", err)
		}

		return nil

	case v1alpha1.GitOpsEngineArgoCD:
		if r.factories.ArgoCD == nil {
			return setup.ErrArgoCDInstallerFactoryNil
		}

		return r.uninstallWithFactory(ctx, r.factories.ArgoCD)

	default:
		return nil
	}
}

// reconcileFluxVersion re-asserts the FluxInstance so a changed
// spec.workload.flux.distributionVersion (or a newly repo-declared FluxInstance)
// takes effect in-place on cluster update. Flux only — ArgoCD has no equivalent
// distribution version.
func (r *componentReconciler) reconcileFluxVersion(
	ctx context.Context,
	_ clusterupdate.Change,
) error {
	if r.clusterCfg.Spec.Cluster.GitOpsEngine != v1alpha1.GitOpsEngineFlux {
		return nil
	}

	kubeconfigPath, err := kubeconfig.GetKubeconfigPathFromConfig(r.clusterCfg)
	if err != nil {
		return fmt.Errorf("failed to get kubeconfig path: %w", err)
	}

	registryHost, err := setup.ResolveRegistryHostForCluster(ctx, r.clusterCfg, r.clusterName)
	if err != nil {
		return fmt.Errorf("resolve registry host for flux: %w", err)
	}

	err = fluxinstaller.SetupInstance(
		ctx,
		kubeconfigPath,
		r.clusterCfg,
		r.clusterName,
		registryHost,
	)
	if err != nil {
		return fmt.Errorf("setup flux instance: %w", err)
	}

	return nil
}

// reconcileWorkloadTag updates the GitOps sync resource (FluxInstance or ArgoCD
// Application) to match the desired workload tag from configuration.
//
//nolint:exhaustive // Only Flux and ArgoCD have sync resources to update
func (r *componentReconciler) reconcileWorkloadTag(
	ctx context.Context,
	_ clusterupdate.Change,
) error {
	gitOpsEngine := r.clusterCfg.Spec.Cluster.GitOpsEngine

	kubeconfigPath, err := kubeconfig.GetKubeconfigPathFromConfig(r.clusterCfg)
	if err != nil {
		return fmt.Errorf("failed to get kubeconfig path: %w", err)
	}

	switch gitOpsEngine {
	case v1alpha1.GitOpsEngineFlux:
		// Resolve registry host for VCluster (others return empty string)
		registryHost, resolveErr := setup.ResolveRegistryHostForCluster(
			ctx, r.clusterCfg, r.clusterName,
		)
		if resolveErr != nil {
			return fmt.Errorf("resolve registry host for flux: %w", resolveErr)
		}

		err = fluxinstaller.SetupInstance(
			ctx, kubeconfigPath, r.clusterCfg, r.clusterName, registryHost,
		)
		if err != nil {
			return fmt.Errorf("setup flux instance: %w", err)
		}

		return nil

	case v1alpha1.GitOpsEngineArgoCD:
		err = setup.EnsureArgoCDResources(
			ctx, kubeconfigPath, r.clusterCfg, r.clusterName,
		)
		if err != nil {
			return fmt.Errorf("ensure argocd resources: %w", err)
		}

		return nil

	default:
		return nil
	}
}

// uninstallWithFactory creates an installer from the factory and calls Uninstall.
func (r *componentReconciler) uninstallWithFactory(
	ctx context.Context,
	factory func(*v1alpha1.Cluster) (installer.Installer, error),
) error {
	inst, err := factory(r.clusterCfg)
	if err != nil {
		return fmt.Errorf("failed to create installer for uninstall: %w", err)
	}

	err = inst.Uninstall(ctx)
	if err != nil {
		return fmt.Errorf("failed to uninstall component: %w", err)
	}

	return nil
}
