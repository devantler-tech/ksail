package flux

import (
	"errors"
	"fmt"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/client/reconciler"
)

// Reconciler errors.
var (
	// ErrReconcileTimeout is returned when reconciliation times out.
	ErrReconcileTimeout = errors.New(
		"timeout waiting for flux kustomization reconciliation - " +
			"verify cluster health, Flux controllers status, and network/connectivity to the cluster",
	)
	// ErrOCIRepositoryNotReady is returned when the OCIRepository is not ready.
	ErrOCIRepositoryNotReady = errors.New(
		"flux OCIRepository is not ready - ensure you have pushed an artifact with 'ksail workload push'",
	)
	// ErrOCIRepositoryReconcileNotHandled is recorded while the source controller
	// has not yet handled the reconcile request we triggered. Surfaced on timeout
	// so the cause — the just-pushed artifact was never served — is visible instead
	// of the generic not-ready error.
	ErrOCIRepositoryReconcileNotHandled = errors.New(
		"flux OCIRepository has not handled the triggered reconcile request - " +
			"the pushed artifact was not served before the timeout",
	)
	// ErrOCIRepositoryReconcileInProgress is recorded while the source controller
	// is still reconciling the request we triggered (Reconciling=True), so its
	// Ready condition still reflects the previous artifact. Surfaced on timeout for
	// the same diagnostic reason as ErrOCIRepositoryReconcileNotHandled.
	ErrOCIRepositoryReconcileInProgress = errors.New(
		"flux OCIRepository is still reconciling the triggered request - " +
			"the pushed artifact was not served before the timeout",
	)
	// ErrKustomizationFailed is returned when the Kustomization reconciliation fails.
	ErrKustomizationFailed = errors.New(
		"flux kustomization reconciliation failed - check the Kustomization status and Flux controller logs for details",
	)
)

// Condition type and status constants shared across the OCIRepository,
// Kustomization, and HelmRelease readiness evaluators.
const (
	conditionTypeReady       = "Ready"
	conditionTypeStalled     = "Stalled"
	conditionTypeReconciling = "Reconciling"
	conditionStatusTrue      = "True"
	conditionStatusFalse     = "False"
)

// Reconciler handles Flux reconciliation operations.
type Reconciler struct {
	*reconciler.Base
}

// newFromBase creates a Reconciler from a base reconciler.
func newFromBase(base *reconciler.Base) *Reconciler {
	return &Reconciler{Base: base}
}

// NewReconciler creates a new Flux reconciler from kubeconfig path.
func NewReconciler(kubeconfigPath string) (*Reconciler, error) {
	r, err := reconciler.New(kubeconfigPath, newFromBase)
	if err != nil {
		return nil, fmt.Errorf("create flux reconciler: %w", err)
	}

	return r, nil
}

// ReconcileOptions configures the reconciliation behavior.
type ReconcileOptions struct {
	// Timeout for waiting for OCIRepository readiness and Kustomization reconciliation.
	Timeout time.Duration
}
