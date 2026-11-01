package kyvernoinstaller

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/fsutil"
	"github.com/devantler-tech/ksail/v7/pkg/k8s"
	"github.com/devantler-tech/ksail/v7/pkg/k8s/readiness"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	// kyvernoPolicyWebhookName is always registered, including in clusters with no
	// mutation policies. It must contain CA-bearing entries before readiness passes.
	kyvernoPolicyWebhookName = "kyverno-policy-mutating-webhook-cfg"

	// kyvernoResourceWebhookName may legitimately have no entries when no policies
	// apply. Any entries it does contain must retain their CA-publication check.
	kyvernoResourceWebhookName = "kyverno-resource-mutating-webhook-cfg"
)

// errNoTimeRemaining is returned when the context deadline has already elapsed
// before the webhook readiness poll can begin.
var errNoTimeRemaining = errors.New("no time remaining for webhook readiness check")

// webhookReadinessTimeout bounds the independent caBundle publication check
// after Helm installation. Install retains its existing best-effort timeout
// behavior; expiry does not establish TLS readiness or guarantee that policy
// admission requests will succeed.
//
//nolint:gochecknoglobals // allows overriding in tests via export_test.go
var webhookReadinessTimeout = 5 * time.Minute

// newClientsetFn is the factory used to create a Kubernetes clientset. The
// kubeconfig path is canonicalised (symlinks resolved) before use.
//
//nolint:gochecknoglobals // allows overriding in tests
var newClientsetFn = func(kubeconfig, kubecontext string) (kubernetes.Interface, error) {
	canonical, err := fsutil.EvalCanonicalPath(kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("canonicalize kubeconfig path: %w", err)
	}

	return k8s.NewClientset(canonical, kubecontext)
}

// waitForWebhookReady polls the always-registered policy webhook configuration
// and the resource configuration in each iteration. Policy entries must exist;
// every policy or resource entry must have a non-empty caBundle. An existing,
// empty resource configuration is valid without applicable policies. Helm readiness can
// precede asynchronous CA publication. This observation does not establish
// policy-rule convergence or verify an admission request.
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

	err = readiness.PollForReadiness(ctx, remaining, func(pollCtx context.Context) (bool, error) {
		return kyvernoWebhookCAsReady(pollCtx, clientset)
	})
	if err != nil {
		// Preserve the caller's existing best-effort deadline handling. The REST
		// rate limiter may decline a wait that would exceed the deadline without
		// wrapping context.DeadlineExceeded. Cancellation remains a distinct error.
		if isDeadlineError(ctx, err) {
			return fmt.Errorf("polling Kyverno webhook readiness: %w", context.DeadlineExceeded)
		}

		return fmt.Errorf("polling Kyverno webhook readiness: %w", err)
	}

	return nil
}

// kyvernoWebhookCAsReady observes policy and resource CA publication together.
// Empty resource entries are valid only alongside a populated policy configuration.
func kyvernoWebhookCAsReady(ctx context.Context, clientset kubernetes.Interface) (bool, error) {
	ready := true

	for _, name := range []string{kyvernoPolicyWebhookName, kyvernoResourceWebhookName} {
		config, err := clientset.AdmissionregistrationV1().
			MutatingWebhookConfigurations().
			Get(ctx, name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			ready = false

			continue
		}

		if err != nil {
			return false, fmt.Errorf("getting Kyverno webhook configuration %q: %w", name, err)
		}

		if name == kyvernoPolicyWebhookName && len(config.Webhooks) == 0 {
			ready = false
		}

		for _, webhook := range config.Webhooks {
			if len(webhook.ClientConfig.CABundle) == 0 {
				ready = false
			}
		}
	}

	return ready, nil
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
