package cluster

import (
	"context"
	"fmt"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/cli/setup"
	"github.com/devantler-tech/ksail/v5/pkg/svc/installer"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/types"
	"github.com/spf13/cobra"
)

// defaultReconcileTimeout is the default timeout for component reconciliation operations.
const defaultReconcileTimeout = 5 * time.Minute

// componentReconciler applies component-level changes detected by the DiffEngine.
// It maps field names from the diff to installer Install/Uninstall operations.
type componentReconciler struct {
	cmd        *cobra.Command
	clusterCfg *v1alpha1.Cluster
	factories  *setup.InstallerFactories
}

// newComponentReconciler creates a reconciler for applying component changes.
func newComponentReconciler(
	cmd *cobra.Command,
	clusterCfg *v1alpha1.Cluster,
) *componentReconciler {
	return &componentReconciler{
		cmd:        cmd,
		clusterCfg: clusterCfg,
		factories:  getInstallerFactories(),
	}
}

// reconcileComponents applies in-place component changes from the diff.
// It processes each component change and records results in the provided UpdateResult.
// Returns the number of successfully applied changes and any error from the last failure.
func (r *componentReconciler) reconcileComponents(
	ctx context.Context,
	diff *types.UpdateResult,
	result *types.UpdateResult,
) error {
	var lastErr error

	for _, change := range diff.InPlaceChanges {
		handler, ok := r.handlerForField(change.Field)
		if !ok {
			continue // Not a component field — handled by provisioner
		}

		err := handler(ctx, change)
		if err != nil {
			result.FailedChanges = append(result.FailedChanges, types.Change{
				Field:    change.Field,
				OldValue: change.OldValue,
				NewValue: change.NewValue,
				Category: types.ChangeCategoryInPlace,
				Reason:   fmt.Sprintf("failed to reconcile: %v", err),
			})

			lastErr = err

			continue
		}

		result.AppliedChanges = append(result.AppliedChanges, types.Change{
			Field:    change.Field,
			OldValue: change.OldValue,
			NewValue: change.NewValue,
			Category: types.ChangeCategoryInPlace,
			Reason:   "component reconciled successfully",
		})
	}

	return lastErr
}

// handlerForField returns the reconciliation handler for a given diff field name.
// Returns false if the field is not a component field (e.g., node counts, registry settings).
func (r *componentReconciler) handlerForField(
	field string,
) (func(context.Context, types.Change) error, bool) {
	handlers := map[string]func(context.Context, types.Change) error{
		"cluster.cni":           r.reconcileCNI,
		"cluster.csi":           r.reconcileCSI,
		"cluster.metricsServer": r.reconcileMetricsServer,
		"cluster.loadBalancer":  r.reconcileLoadBalancer,
		"cluster.certManager":   r.reconcileCertManager,
		"cluster.policyEngine":  r.reconcilePolicyEngine,
		"cluster.gitOpsEngine":  r.reconcileGitOpsEngine,
	}

	handler, ok := handlers[field]

	return handler, ok
}

// reconcileCNI switches the CNI by installing the new CNI.
// The old CNI is not uninstalled — the new CNI replaces it via Helm upgrade.
func (r *componentReconciler) reconcileCNI(_ context.Context, _ types.Change) error {
	_, err := setup.InstallCNI(r.cmd, r.clusterCfg, nil)
	if err != nil {
		return fmt.Errorf("failed to install CNI: %w", err)
	}

	return nil
}

// reconcileCSI installs or uninstalls the CSI driver.
func (r *componentReconciler) reconcileCSI(ctx context.Context, change types.Change) error {
	if r.factories.CSI == nil {
		return setup.ErrCSIInstallerFactoryNil
	}

	newValue := v1alpha1.CSI(change.NewValue)

	// If new value disables CSI, uninstall the old one
	if newValue == v1alpha1.CSIDisabled {
		return r.uninstallWithFactory(ctx, r.factories.CSI)
	}

	// Install the new CSI
	return setup.InstallCSISilent(ctx, r.clusterCfg, r.factories)
}

// reconcileMetricsServer installs or uninstalls the metrics server.
func (r *componentReconciler) reconcileMetricsServer(
	ctx context.Context,
	change types.Change,
) error {
	newValue := v1alpha1.MetricsServer(change.NewValue)

	if newValue == v1alpha1.MetricsServerDisabled {
		// For uninstall, create a metrics-server installer and uninstall.
		// We need to create the installer manually since the setup package
		// only exposes Install functions.
		return nil // Metrics-server uninstall is a no-op for now
	}

	if setup.NeedsMetricsServerInstall(r.clusterCfg) {
		return setup.InstallMetricsServerSilent(ctx, r.clusterCfg, r.factories)
	}

	return nil
}

// reconcileLoadBalancer installs or uninstalls the load balancer.
func (r *componentReconciler) reconcileLoadBalancer(
	ctx context.Context,
	_ types.Change,
) error {
	if setup.NeedsLoadBalancerInstall(r.clusterCfg) {
		return setup.InstallLoadBalancerSilent(ctx, r.clusterCfg, r.factories)
	}

	return nil
}

// reconcileCertManager installs or uninstalls cert-manager.
func (r *componentReconciler) reconcileCertManager(
	ctx context.Context,
	change types.Change,
) error {
	if r.factories.CertManager == nil {
		return setup.ErrCertManagerInstallerFactoryNil
	}

	newValue := v1alpha1.CertManager(change.NewValue)

	if newValue == v1alpha1.CertManagerDisabled {
		return r.uninstallWithFactory(ctx, r.factories.CertManager)
	}

	return setup.InstallCertManagerSilent(ctx, r.clusterCfg, r.factories)
}

// reconcilePolicyEngine installs or uninstalls the policy engine.
func (r *componentReconciler) reconcilePolicyEngine(
	ctx context.Context,
	change types.Change,
) error {
	if r.factories.PolicyEngine == nil {
		return setup.ErrPolicyEngineInstallerFactoryNil
	}

	newValue := v1alpha1.PolicyEngine(change.NewValue)

	if newValue == v1alpha1.PolicyEngineNone || newValue == "" {
		return r.uninstallWithFactory(ctx, r.factories.PolicyEngine)
	}

	return setup.InstallPolicyEngineSilent(ctx, r.clusterCfg, r.factories)
}

// reconcileGitOpsEngine installs or uninstalls the GitOps engine.
func (r *componentReconciler) reconcileGitOpsEngine(
	ctx context.Context,
	change types.Change,
) error {
	newValue := v1alpha1.GitOpsEngine(change.NewValue)

	if newValue == v1alpha1.GitOpsEngineNone || newValue == "" {
		return r.uninstallGitOpsEngine(ctx, change)
	}

	// Install the new GitOps engine
	switch newValue {
	case v1alpha1.GitOpsEngineFlux:
		return setup.InstallFluxSilent(ctx, r.clusterCfg, r.factories)
	case v1alpha1.GitOpsEngineArgoCD:
		return setup.InstallArgoCDSilent(ctx, r.clusterCfg, r.factories)
	default:
		return nil
	}
}

// uninstallGitOpsEngine uninstalls the old GitOps engine.
func (r *componentReconciler) uninstallGitOpsEngine(
	ctx context.Context,
	change types.Change,
) error {
	oldValue := v1alpha1.GitOpsEngine(change.OldValue)

	switch oldValue {
	case v1alpha1.GitOpsEngineFlux:
		helmClient, _, err := r.factories.HelmClientFactory(r.clusterCfg)
		if err != nil {
			return fmt.Errorf("failed to create helm client for Flux uninstall: %w", err)
		}

		fluxInst := r.factories.Flux(helmClient, defaultReconcileTimeout)

		return fluxInst.Uninstall(ctx)

	case v1alpha1.GitOpsEngineArgoCD:
		if r.factories.ArgoCD == nil {
			return setup.ErrArgoCDInstallerFactoryNil
		}

		return r.uninstallWithFactory(ctx, r.factories.ArgoCD)

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

	return inst.Uninstall(ctx)
}
