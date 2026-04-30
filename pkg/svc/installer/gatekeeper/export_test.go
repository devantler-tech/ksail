package gatekeeperinstaller

import (
	"context"
	"time"

	"k8s.io/client-go/kubernetes"
)

// SetWaitForWebhookReadyFn replaces the production webhook-readiness wait with
// a test-controlled stub. Call the returned cleanup function to restore the
// original implementation.
func SetWaitForWebhookReadyFn(
	fn func(ctx context.Context, kubeconfig, kubeContext string, deadline time.Duration) error,
) func() {
	orig := waitForWebhookReadyFn
	waitForWebhookReadyFn = fn

	return func() { waitForWebhookReadyFn = orig }
}

// WaitForGatekeeperWebhookReady exposes the internal polling logic for unit
// testing without a package-level variable replacement.
func WaitForGatekeeperWebhookReady(
	ctx context.Context,
	clientset kubernetes.Interface,
	deadline time.Duration,
) error {
	return waitForGatekeeperWebhookReady(ctx, clientset, deadline)
}
