package gatekeeperinstaller

import (
	"context"
	"fmt"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	"github.com/devantler-tech/ksail/v7/pkg/k8s"
	"github.com/devantler-tech/ksail/v7/pkg/k8s/readiness"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	err := readiness.PollForReadiness(ctx, deadline, func(ctx context.Context) (bool, error) {
		vwcClient := clientset.AdmissionregistrationV1().ValidatingWebhookConfigurations()

		webhookCfg, err := vwcClient.Get(ctx, gatekeeperValidatingWebhookName, metav1.GetOptions{})
		if err != nil {
			if k8serrors.IsNotFound(err) {
				// Webhook config not yet created — keep polling.
				return false, nil
			}

			return false, fmt.Errorf("get %s: %w", gatekeeperValidatingWebhookName, err)
		}

		if len(webhookCfg.Webhooks) == 0 {
			return false, nil
		}

		for _, webhook := range webhookCfg.Webhooks {
			if len(webhook.ClientConfig.CABundle) == 0 {
				return false, nil
			}
		}

		return true, nil
	})
	if err != nil {
		return fmt.Errorf("poll gatekeeper webhook readiness: %w", err)
	}

	return nil
}
