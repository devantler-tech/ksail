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

// controlPlaneComponents returns the ArgoCD control-plane Deployments whose
// cold-start (not-yet-Ready) state produces the transient signals — "connection
// refused" (redis), "unable to resolve"/"failed to fetch" (repo-server) — that the
// app-sync poll would otherwise misclassify as a permanent source-unavailable
// failure. The gate waits for them before the first poll so the cold-start window
// is closed at the root. Absent components (custom or renamed installs, e.g. HA
// redis) are tolerated and skipped.
func controlPlaneComponents() []string {
	return []string{"argocd-repo-server", "argocd-redis", "argocd-server"}
}

// WaitForControlPlaneReady waits (bounded) for the ArgoCD control-plane Deployments
// to be Ready before app-sync polling begins, so a just-started repo-server/redis
// does not emit transient errors that ksail's poll loop treats as a permanent
// source-unavailable failure (issue #5948).
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

	for _, name := range controlPlaneComponents() {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return fmt.Errorf(
				"%w (last: %s/%s)",
				errControlPlaneReadyBudgetExhausted, DefaultNamespace, name,
			)
		}

		err := readiness.WaitForDeploymentReadyIfExists(
			ctx, clientset, DefaultNamespace, name, remaining,
		)
		if err != nil {
			return fmt.Errorf(
				"argocd control-plane component %s/%s not ready: %w",
				DefaultNamespace, name, err,
			)
		}
	}

	return nil
}
