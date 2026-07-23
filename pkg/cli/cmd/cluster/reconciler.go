package cluster

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/kubeconfig"
	"github.com/devantler-tech/ksail/v7/pkg/cli/setup"
	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	specdiff "github.com/devantler-tech/ksail/v7/pkg/svc/diff"
	"github.com/devantler-tech/ksail/v7/pkg/svc/installer"
	fluxinstaller "github.com/devantler-tech/ksail/v7/pkg/svc/installer/flux"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	"github.com/devantler-tech/ksail/v7/pkg/svc/state"
	"github.com/spf13/cobra"
)

// defaultReconcileTimeout is the default timeout for component reconciliation operations.
const defaultReconcileTimeout = 5 * time.Minute

// errMetricsServerDisableUnsupported is returned when attempting to disable metrics-server in-place.
var errMetricsServerDisableUnsupported = errors.New(
	"disabling metrics-server in-place is not yet supported; use 'ksail cluster delete && ksail cluster create'",
)

// errEKSLoadBalancerControllerOwnershipRequired reports that desired removal
// cannot converge while a live release lacks exact-region KSail ownership.
var errEKSLoadBalancerControllerOwnershipRequired = errors.New(
	"AWS load balancer controller ownership is unresolved; preserve the live release",
)

var errEKSLoadBalancerControllerMutationSkipped = errors.New(
	"AWS load balancer controller update was skipped; desired state remains unresolved",
)

// componentReconciler applies component-level changes detected by the DiffEngine.
// It maps field names from the diff to installer Install/Uninstall operations.
type componentReconciler struct {
	cmd         *cobra.Command
	clusterCfg  *v1alpha1.Cluster
	clusterName string
	eksRegion   string
	factories   *setup.InstallerFactories
	// autoscalerReconciled tracks whether the cluster autoscaler has already been
	// reconciled during this update pass. Multiple diff fields share the
	// "cluster.autoscaler.node." prefix and map to a single Helm operation;
	// this flag deduplicates the install/upgrade/uninstall call.
	// autoscalerErr preserves the error from the first attempt so that
	// subsequent calls surface the same failure instead of silently succeeding.
	autoscalerReconciled bool
	autoscalerErr        error
	// loadBalancerReconciled coalesces the generic load-balancer field and the
	// EKS-specific controller opt-in when both change in one update pass.
	loadBalancerReconciled bool
	loadBalancerErr        error
	// eksLoadBalancerOwnershipUpdated is set only after this update pass actually
	// installs/upgrades or uninstalls the EKS controller. If untouched, final state
	// persistence preserves the prior exact-region ownership marker.
	eksLoadBalancerOwnershipUpdated bool
	eksLoadBalancerManaged          bool
	eksLoadBalancerReleaseIdentity  string
}

// newComponentReconciler creates a reconciler for applying component changes.
func newComponentReconciler(
	cmd *cobra.Command,
	clusterCfg *v1alpha1.Cluster,
	clusterName string,
	eksRegion ...string,
) *componentReconciler {
	region := ""
	if len(eksRegion) > 0 {
		region = strings.TrimSpace(eksRegion[0])
	}

	return &componentReconciler{
		cmd:         cmd,
		clusterCfg:  clusterCfg,
		clusterName: clusterName,
		eksRegion:   region,
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
	handlers[specdiff.EKSLoadBalancerControllerField] = r.reconcileLoadBalancer

	if handler, ok := handlers[field]; ok {
		return handler, true
	}

	// Prefix-based matching for fields with dynamic suffixes.
	// All cluster.autoscaler.node.* fields (enabled, maxNodesTotal, expander,
	// scaleDownUnneededTime, capacityBuffers, pools[...]) map to a single Helm
	// install/upgrade.
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
	change clusterupdate.Change,
) error {
	// A generic EKS load-balancer change with the controller opt-in disabled is
	// intentionally a no-op. Do not consume the coalescing slot here: when the
	// opt-in also changed, its later dedicated diff must still reach uninstall.
	if r.clusterCfg.Spec.Cluster.Distribution == v1alpha1.DistributionEKS &&
		change.Field != specdiff.EKSLoadBalancerControllerField &&
		!r.clusterCfg.Spec.Cluster.EKS.ExperimentalAWSLoadBalancerController {
		return nil
	}

	if r.loadBalancerReconciled {
		return r.loadBalancerErr
	}

	r.loadBalancerReconciled = true
	r.loadBalancerErr = r.doReconcileLoadBalancer(ctx)

	return r.loadBalancerErr
}

func (r *componentReconciler) doReconcileLoadBalancer(
	ctx context.Context,
) error {
	if setup.NeedsLoadBalancerInstall(r.clusterCfg) {
		controllerManaged, releaseIdentity, err := r.installLoadBalancer(ctx)
		if err != nil {
			return fmt.Errorf("failed to install load balancer: %w", err)
		}

		if r.clusterCfg.Spec.Cluster.Distribution == v1alpha1.DistributionEKS {
			if !controllerManaged {
				return errEKSLoadBalancerControllerMutationSkipped
			}

			r.eksLoadBalancerOwnershipUpdated = true
			r.eksLoadBalancerManaged = true
			r.eksLoadBalancerReleaseIdentity = releaseIdentity
		}

		return nil
	}

	if r.clusterCfg.Spec.Cluster.Distribution != v1alpha1.DistributionEKS {
		return nil
	}

	managed, releaseIdentity, err := r.eksLoadBalancerControllerOwnership()
	if err != nil {
		return err
	}

	if !managed {
		// A live Helm release without exact-region KSail ownership state may be
		// manually managed. Preserve it, but report unresolved convergence so the
		// requested removal is not recorded in the applied baseline as success.
		return errEKSLoadBalancerControllerOwnershipRequired
	}

	err = setup.UninstallEKSLoadBalancerControllerSilent(
		ctx,
		r.clusterCfg,
		r.factories,
		releaseIdentity,
	)
	if err != nil && (errors.Is(err, helm.ErrReleaseNotFound) ||
		errors.Is(err, helm.ErrNoReleaseStorage)) {
		r.eksLoadBalancerOwnershipUpdated = true
		r.eksLoadBalancerManaged = false
		r.eksLoadBalancerReleaseIdentity = ""

		return nil
	}

	if err != nil {
		return fmt.Errorf("failed to uninstall load balancer: %w", err)
	}

	r.eksLoadBalancerOwnershipUpdated = true
	r.eksLoadBalancerManaged = false
	r.eksLoadBalancerReleaseIdentity = ""

	return nil
}

func (r *componentReconciler) installLoadBalancer(ctx context.Context) (bool, string, error) {
	if r.clusterCfg.Spec.Cluster.Distribution == v1alpha1.DistributionEKS {
		managed, releaseIdentity, err := setup.InstallEKSLoadBalancerControllerWithResult(
			ctx,
			r.clusterCfg,
			r.factories,
		)
		if err != nil {
			return false, "", fmt.Errorf("install EKS load balancer controller: %w", err)
		}

		return managed, releaseIdentity, nil
	}

	err := setup.InstallLoadBalancerSilent(ctx, r.clusterCfg, r.factories)
	if err != nil {
		return false, "", fmt.Errorf("install load balancer: %w", err)
	}

	return false, "", nil
}

func (r *componentReconciler) eksLoadBalancerControllerOwnershipAfterReconcile() (
	bool,
	string,
	error,
) {
	if r.eksLoadBalancerOwnershipUpdated {
		return r.eksLoadBalancerManaged, r.eksLoadBalancerReleaseIdentity, nil
	}

	return r.eksLoadBalancerControllerOwnership()
}

func (r *componentReconciler) eksLoadBalancerControllerOwnership() (bool, string, error) {
	if r.eksRegion == "" {
		return false, "", nil
	}

	snapshot, err := state.LoadEKSComponentState(r.clusterName, r.eksRegion)
	if err != nil {
		if errors.Is(err, state.ErrEKSComponentStateNotFound) {
			return false, "", nil
		}

		return false, "", fmt.Errorf("verify AWS load balancer controller ownership: %w", err)
	}

	return snapshot.AWSLoadBalancerControllerManaged,
		snapshot.AWSLoadBalancerControllerReleaseIdentity,
		nil
}

func (r *componentReconciler) hasEKSLoadBalancerOwnershipUpdate() bool {
	return r != nil && r.eksLoadBalancerOwnershipUpdated
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

	if newValue.IsNone() {
		// If already none/disabled, nothing to uninstall
		if oldValue.IsNone() {
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
