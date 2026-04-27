package kyvernoinstaller

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/k8s"
	"github.com/devantler-tech/ksail/v7/pkg/k8s/readiness"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	// kyvernoResourceWebhookName is the MutatingWebhookConfiguration that intercepts
	// all resource operations (create/update/delete). Kyverno populates its caBundle
	// once the admission controller has initialised its TLS certificate — a non-empty
	// caBundle is therefore a reliable signal that the webhook is ready to serve.
	kyvernoResourceWebhookName = "kyverno-resource-mutating-webhook-cfg"
)

// errNoTimeRemaining is returned when the context deadline has already elapsed
// before the webhook readiness poll can begin.
var errNoTimeRemaining = errors.New("no time remaining for webhook readiness check")

// webhookReadinessTimeout is the independent deadline given to the webhook caBundle
// readiness check after Helm install. The wait is best-effort: if the caBundle is not
// populated within this window, Install returns success anyway because
// failurePolicy: Ignore ensures API operations proceed even without a fully initialised
// webhook. A dedicated constant (shorter than KyvernoInstallTimeout) prevents the
// best-effort check from consuming an excessive share of the CI job's time budget in
// environments where informer cache sync takes longer than expected.
//
//nolint:gochecknoglobals // allows overriding in tests via export_test.go
var webhookReadinessTimeout = 5 * time.Minute

// newClientsetFn is the factory used to create a Kubernetes clientset.
//
//nolint:gochecknoglobals // allows overriding in tests
var newClientsetFn = func(kubeconfig, kubecontext string) (kubernetes.Interface, error) {
	return k8s.NewClientset(kubeconfig, kubecontext)
}

// waitForWebhookReady polls the Kyverno MutatingWebhookConfiguration until every
// webhook entry has a non-empty caBundle. This guards against the race condition
// where Helm reports the chart as installed and the admission controller pods are
// Running/Ready, but the controller has not yet initialised its TLS certificate and
// injected it into the webhook configuration. Any workload operation that triggers
// the webhook before the caBundle is set will fail.
//
// ctx should carry its own independent deadline (typically timeout) so that the
// webhook wait does not compete with the Helm install for the same budget.
func (i *Installer) waitForWebhookReady(ctx context.Context) error {
	remaining := i.timeout // fallback when ctx has no deadline
	if dl, ok := ctx.Deadline(); ok {
		remaining = time.Until(dl)
		if remaining <= 0 {
			return errNoTimeRemaining
		}
	}

	clientset, err := newClientsetFn(i.kubeconfig, i.kubecontext)
	if err != nil {
		return fmt.Errorf("creating clientset for Kyverno webhook readiness check: %w", err)
	}

	err = readiness.PollForReadiness(ctx, remaining, func(ctx context.Context) (bool, error) {
		webhook, err := clientset.AdmissionregistrationV1().
			MutatingWebhookConfigurations().
			Get(ctx, kyvernoResourceWebhookName, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				// Not yet created — keep polling
				return false, nil
			}

			return false, fmt.Errorf(
				"getting MutatingWebhookConfiguration %q: %w",
				kyvernoResourceWebhookName,
				err,
			)
		}

		if len(webhook.Webhooks) == 0 {
			return false, nil
		}

		for _, wh := range webhook.Webhooks {
			if len(wh.ClientConfig.CABundle) == 0 {
				return false, nil
			}
		}

		return true, nil
	})
	if err != nil {
		return fmt.Errorf("polling Kyverno webhook readiness: %w", err)
	}

	return nil
}
