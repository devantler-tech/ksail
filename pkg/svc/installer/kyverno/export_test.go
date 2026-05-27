package kyvernoinstaller

import (
	"time"

	"k8s.io/client-go/kubernetes"
)

// IsDeadlineErrorForTest exposes isDeadlineError for unit testing.
//
//nolint:gochecknoglobals // test seam for the deadline-error classifier
var IsDeadlineErrorForTest = isDeadlineError

// SetNewClientsetFn overrides the clientset factory for testing.
// Returns a cleanup function that restores the original factory.
func SetNewClientsetFn(
	fn func(kubeconfig, kubecontext string) (kubernetes.Interface, error),
) func() {
	original := newClientsetFn
	newClientsetFn = fn

	return func() { newClientsetFn = original }
}

// SetWebhookReadinessTimeout overrides the webhook readiness timeout for testing.
// Returns a cleanup function that restores the original timeout.
func SetWebhookReadinessTimeout(d time.Duration) func() {
	original := webhookReadinessTimeout
	webhookReadinessTimeout = d

	return func() { webhookReadinessTimeout = original }
}
