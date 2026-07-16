package argocd

import (
	"context"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/client/reconciler"
	"k8s.io/client-go/kubernetes"
)

// NewReconcilerWithClientForTest builds a Reconciler with a dummy base so the
// WaitForControlPlaneReady method can be exercised with an injected clientset seam.
func NewReconcilerWithClientForTest() *Reconciler {
	return &Reconciler{Base: &reconciler.Base{KubeconfigPath: "unused"}}
}

// WaitForControlPlaneReady exports waitForControlPlaneReady for testing.
func WaitForControlPlaneReady(
	ctx context.Context,
	clientset kubernetes.Interface,
	timeout time.Duration,
) error {
	return waitForControlPlaneReady(ctx, clientset, timeout)
}

// SetNewControlPlaneClientset replaces newControlPlaneClientset with a stub and
// returns a restore func.
func SetNewControlPlaneClientset(
	fn func(string) (kubernetes.Interface, error),
) func() {
	original := newControlPlaneClientset
	newControlPlaneClientset = fn

	return func() {
		newControlPlaneClientset = original
	}
}
