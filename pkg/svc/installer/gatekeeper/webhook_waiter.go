package gatekeeperinstaller

import (
	"context"
	"fmt"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	"github.com/devantler-tech/ksail/v7/pkg/k8s"
	"github.com/devantler-tech/ksail/v7/pkg/svc/installer/internal/webhookwait"
	"k8s.io/client-go/kubernetes"
)

const (
	// gatekeeperValidatingWebhookName is the name of the ValidatingWebhookConfiguration
	// that Gatekeeper registers to enforce policies. It is created by the Helm chart and
	// populated with a caBundle by the cert-controller sidecar; subsequent admission calls
	// will fail (or be retried slowly) until the caBundle is set, causing intermittent
	// readiness-probe context-cancellation errors on the first workload pods.
	gatekeeperValidatingWebhookName = "gatekeeper-validating-webhook-configuration"
)

// waitForWebhookReadyFn is the production webhook-readiness wait used by [Installer.Install].
// It is held in a package-level variable so that tests can substitute a fake
// without constructing a real cluster or clientset.
//
//nolint:gochecknoglobals // test seam for the webhook readiness wait
var waitForWebhookReadyFn = func(
	ctx context.Context,
	kubeconfig, kubeContext string,
	deadline time.Duration,
) error {
	canonical, err := fsutil.EvalCanonicalPath(kubeconfig)
	if err != nil {
		return fmt.Errorf("canonicalize kubeconfig path: %w", err)
	}

	clientset, err := k8s.NewClientset(canonical, kubeContext)
	if err != nil {
		return fmt.Errorf("create kubernetes client for gatekeeper webhook readiness: %w", err)
	}

	return waitForGatekeeperWebhookReady(ctx, clientset, deadline)
}

// waitForGatekeeperWebhookReady polls the ValidatingWebhookConfiguration until every
// webhook entry has a non-empty caBundle. An empty caBundle means the Gatekeeper
// cert-controller has not yet injected the TLS certificate, so admission calls to
// the webhook endpoint may time out or be retried by the API server.
func waitForGatekeeperWebhookReady(
	ctx context.Context,
	clientset kubernetes.Interface,
	deadline time.Duration,
) error {
	//nolint:wrapcheck // webhookwait.Poll already wraps with the readiness description
	return webhookwait.Poll(
		ctx,
		clientset,
		webhookwait.Validating,
		gatekeeperValidatingWebhookName,
		"poll gatekeeper webhook readiness",
		deadline,
	)
}
