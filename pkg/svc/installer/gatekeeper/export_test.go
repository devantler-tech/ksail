package gatekeeperinstaller

import (
	"context"
	"time"
)

// SetWaitForWebhookReadyFn replaces the production webhook-readiness wait with
// a test-controlled stub. Call the returned cleanup function to restore the
// original implementation.
func SetWaitForWebhookReadyFn(fn func(ctx context.Context, kubeconfig, kubeContext string, deadline time.Duration) error) func() {
	orig := waitForWebhookReadyFn
	waitForWebhookReadyFn = fn

	return func() { waitForWebhookReadyFn = orig }
}

// WaitForGatekeeperWebhookReady exposes the internal polling logic for unit
// testing without a package-level variable replacement.
var WaitForGatekeeperWebhookReady = waitForGatekeeperWebhookReady
