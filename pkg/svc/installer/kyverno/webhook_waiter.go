package kyvernoinstaller

import (
	"context"
	"errors"
	"fmt"
	"strings"
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
// injected it into the webhook configuration. Workload operations that trigger the
// webhook before the caBundle is populated are still admitted because the Kyverno
// webhook is configured with failurePolicy: Ignore, but the caBundle check provides
// an early signal that the controller is fully ready.
//
// ctx should carry an appropriate deadline so the check does not wait indefinitely.
// Callers treat context.DeadlineExceeded as non-fatal (see Installer.Install).
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
		return webhookCABundlesReady(ctx, clientset)
	})
	if err != nil {
		// The webhook readiness wait is best-effort: callers treat a deadline timeout as
		// non-fatal because the webhook uses failurePolicy: Ignore. When the poll runs out
		// the clock, client-go's REST rate limiter can surface the timeout as
		// "client rate limiter Wait returned an error: rate: Wait(n=1) would exceed context
		// deadline" instead of a clean context.DeadlineExceeded (the limiter declines to
		// wait past the imminent deadline). Normalize that variant to context.DeadlineExceeded
		// so the best-effort caller doesn't treat a slow-but-harmless webhook as a fatal error.
		if isDeadlineError(ctx, err) {
			return fmt.Errorf("polling Kyverno webhook readiness: %w", context.DeadlineExceeded)
		}

		return fmt.Errorf("polling Kyverno webhook readiness: %w", err)
	}

	return nil
}

// webhookCABundlesReady reports whether the Kyverno resource MutatingWebhookConfiguration
// exists and every webhook entry has a non-empty caBundle. A not-yet-created config or an
// empty caBundle means the admission controller has not finished initialising its TLS
// certificate, so the poll should continue.
func webhookCABundlesReady(ctx context.Context, clientset kubernetes.Interface) (bool, error) {
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
}

// isDeadlineError reports whether err represents the webhook-readiness deadline being
// reached — either a genuine context.DeadlineExceeded, the context's deadline having
// elapsed, or the client-go rate limiter declining to wait past the imminent deadline
// (which is semantically a timeout but is not wrapped as context.DeadlineExceeded).
//
// Cancellation (context.Canceled) is deliberately NOT treated as a deadline: a caller that
// cancels the install must see that propagate rather than have it normalized to a benign
// best-effort timeout.
func isDeadlineError(ctx context.Context, err error) bool {
	if errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return true
	}

	return strings.Contains(err.Error(), "would exceed context deadline")
}
