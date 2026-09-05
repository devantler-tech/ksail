package argocd

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/k8s"
	"github.com/devantler-tech/ksail/v7/pkg/k8s/readiness"
	"k8s.io/client-go/kubernetes"
)

// maxControlPlaneReadyWait bounds how long the pre-reconcile readiness gate will
// wait for the ArgoCD control-plane, so a genuinely-stuck component cannot starve
// the app-sync poll of its own window. The reconcile --timeout caps it further
// when it is smaller.
const maxControlPlaneReadyWait = 3 * time.Minute

// errControlPlaneReadyBudgetExhausted is returned when the gate's time budget is
// spent before a component could even be checked. It is handled fail-open by the
// caller (a warning, then reconcile proceeds), so it never aborts a reconcile.
var errControlPlaneReadyBudgetExhausted = errors.New(
	"timeout budget exhausted before checking argocd control-plane readiness",
)

// errUnknownControlPlaneComponentKind is returned for a component kind the gate
// has no readiness check for; it can only come from a programming error in
// controlPlaneComponents.
var errUnknownControlPlaneComponentKind = errors.New("unknown argocd control-plane component kind")

// newControlPlaneClientset builds a typed clientset for the readiness gate.
//
//nolint:gochecknoglobals // Allows mocking for tests.
var newControlPlaneClientset = func(kubeconfigPath string) (kubernetes.Interface, error) {
	restConfig, err := k8s.BuildRESTConfig(kubeconfigPath, "")
	if err != nil {
		return nil, fmt.Errorf("build rest config: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("create kubernetes client: %w", err)
	}

	return clientset, nil
}

// controlPlaneComponentKind is the workload kind a control-plane component runs as.
type controlPlaneComponentKind string

const (
	componentDeployment  controlPlaneComponentKind = "deployment"
	componentStatefulSet controlPlaneComponentKind = "statefulset"
)

// controlPlaneComponent names one ArgoCD control-plane workload the gate waits for.
type controlPlaneComponent struct {
	kind controlPlaneComponentKind
	name string
}

// controlPlaneComponents returns the ArgoCD control-plane workloads whose
// cold-start (not-yet-Ready) state produces the transient signals — "connection
// refused" (redis), "unable to resolve"/"failed to fetch" (repo-server) — that the
// app-sync poll would otherwise misclassify as a permanent source-unavailable
// failure, plus the application controller, which is the component that writes
// the Application status the poll reads: until it is Ready a cold-start
// ComparisonError it recorded earlier stays on the Application and can be
// classified before it clears. The gate waits for them before the first poll so
// the cold-start window is closed at the root. Absent components (custom or
// renamed installs, e.g. HA redis) are tolerated and skipped.
func controlPlaneComponents() []controlPlaneComponent {
	return []controlPlaneComponent{
		{kind: componentDeployment, name: "argocd-repo-server"},
		{kind: componentDeployment, name: "argocd-redis"},
		{kind: componentDeployment, name: "argocd-server"},
		{kind: componentStatefulSet, name: "argocd-application-controller"},
	}
}

// ControlPlaneGateBudget returns how much of reconcileTimeout the readiness gate
// may spend before the app-sync poll begins: at most half of it, and never more
// than maxControlPlaneReadyWait. Sharing the reconcile deadline this way keeps one
// reconcile attempt inside its --timeout while still reserving at least half of
// that window for the reconcile itself, so a slow control-plane can delay the poll
// but never starve it. A non-positive timeout falls back to the gate maximum.
func ControlPlaneGateBudget(reconcileTimeout time.Duration) time.Duration {
	if reconcileTimeout <= 0 {
		return maxControlPlaneReadyWait
	}

	return min(reconcileTimeout/2, maxControlPlaneReadyWait)
}

// WaitForControlPlaneReady waits (bounded by timeout, at most
// maxControlPlaneReadyWait) for the ArgoCD control-plane workloads to be Ready
// before app-sync polling begins, so a just-started repo-server/redis does not
// emit transient errors that ksail's poll loop treats as a permanent
// source-unavailable failure, and a cold-start ComparisonError left on an
// Application is not read before the application controller has cleared it
// (issue #5948).
//
// It is intended to be used fail-open: the caller treats a returned error as a
// warning and proceeds with reconcile, so the gate can only reduce the cold-start
// race, never add a failure mode. Genuine source errors are still surfaced by the
// unchanged app-sync poll once the control-plane is ready.
func (r *Reconciler) WaitForControlPlaneReady(ctx context.Context, timeout time.Duration) error {
	clientset, err := newControlPlaneClientset(r.KubeconfigPath)
	if err != nil {
		return err
	}

	return waitForControlPlaneReady(ctx, clientset, timeout)
}

// waitForControlPlaneReady checks each control-plane component in sequence, sharing
// a single bounded time budget. Absent components are tolerated. It returns the
// first readiness error (for precise unit testing); the caller applies the
// fail-open policy.
func waitForControlPlaneReady(
	ctx context.Context,
	clientset kubernetes.Interface,
	timeout time.Duration,
) error {
	budget := timeout
	if budget <= 0 || budget > maxControlPlaneReadyWait {
		budget = maxControlPlaneReadyWait
	}

	deadline := time.Now().Add(budget)

	for _, component := range controlPlaneComponents() {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return fmt.Errorf(
				"%w (last: %s/%s)",
				errControlPlaneReadyBudgetExhausted, DefaultNamespace, component.name,
			)
		}

		err := waitForComponentReady(ctx, clientset, component, remaining)
		if err != nil {
			return fmt.Errorf(
				"argocd control-plane %s %s/%s not ready: %w",
				component.kind, DefaultNamespace, component.name, err,
			)
		}
	}

	return nil
}

// waitForComponentReady waits for one control-plane component by its kind,
// tolerating its absence.
func waitForComponentReady(
	ctx context.Context,
	clientset kubernetes.Interface,
	component controlPlaneComponent,
	remaining time.Duration,
) error {
	switch component.kind {
	case componentStatefulSet:
		return readiness.WaitForStatefulSetReadyIfExists( //nolint:wrapcheck // caller wraps with the component
			ctx, clientset, DefaultNamespace, component.name, remaining,
		)
	case componentDeployment:
		return readiness.WaitForDeploymentReadyIfExists( //nolint:wrapcheck // caller wraps with the component
			ctx, clientset, DefaultNamespace, component.name, remaining,
		)
	default:
		return fmt.Errorf("%w: %q", errUnknownControlPlaneComponentKind, component.kind)
	}
}
