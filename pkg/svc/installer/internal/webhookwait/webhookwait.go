// Package webhookwait provides a shared poller that waits for an admission
// webhook configuration's caBundle to be populated. Admission controllers
// (Kyverno's MutatingWebhookConfiguration, Gatekeeper's
// ValidatingWebhookConfiguration) register their webhook configuration when the
// Helm chart installs, but the controller injects the TLS caBundle
// asynchronously after the pods become Ready. Workload operations that hit the
// webhook before the caBundle is set may time out, so installers poll until
// every webhook entry carries a non-empty caBundle.
package webhookwait

import (
	"context"
	"fmt"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/k8s/readiness"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// Kind selects which admission webhook configuration to poll.
type Kind int

const (
	// Mutating polls a MutatingWebhookConfiguration (e.g. Kyverno's resource
	// mutating webhook).
	Mutating Kind = iota
	// Validating polls a ValidatingWebhookConfiguration (e.g. Gatekeeper's
	// validating webhook).
	Validating
)

// Poll waits until the named admission webhook configuration of the given kind
// exists and every webhook entry has a non-empty caBundle, or the deadline
// elapses. A not-yet-created configuration or an empty caBundle means the
// admission controller has not finished initialising its TLS certificate, so
// the poll continues. The error (when non-nil) is wrapped with the supplied
// description so callers can apply their own (fatal vs. best-effort) policy.
func Poll(
	ctx context.Context,
	clientset kubernetes.Interface,
	kind Kind,
	configName, description string,
	deadline time.Duration,
) error {
	err := readiness.PollForReadiness(ctx, deadline, func(ctx context.Context) (bool, error) {
		return caBundlesReady(ctx, clientset, kind, configName)
	})
	if err != nil {
		return fmt.Errorf("%s: %w", description, err)
	}

	return nil
}

// caBundlesReady reports whether the named webhook configuration exists and
// every webhook entry has a non-empty caBundle. A NotFound configuration or any
// empty caBundle returns (false, nil) so the caller keeps polling.
func caBundlesReady(
	ctx context.Context,
	clientset kubernetes.Interface,
	kind Kind,
	configName string,
) (bool, error) {
	caBundles, err := getCABundles(ctx, clientset, kind, configName)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// Not yet created — keep polling.
			return false, nil
		}

		return false, fmt.Errorf("getting webhook configuration %q: %w", configName, err)
	}

	if len(caBundles) == 0 {
		return false, nil
	}

	for _, caBundle := range caBundles {
		if len(caBundle) == 0 {
			return false, nil
		}
	}

	return true, nil
}

// getCABundles fetches the named webhook configuration of the given kind and
// returns the caBundle of every webhook entry. It returns the raw API error
// (including NotFound) so the caller can distinguish not-yet-created from real
// failures.
func getCABundles(
	ctx context.Context,
	clientset kubernetes.Interface,
	kind Kind,
	configName string,
) ([][]byte, error) {
	// The Mutating and Validating branches are structurally identical but operate
	// on distinct, non-interchangeable API types (MutatingWebhookConfiguration vs
	// ValidatingWebhookConfiguration), so the CABundle extraction cannot be shared
	// without generics/reflection that would obscure it.
	// jscpd:ignore-start
	if kind == Mutating {
		cfg, err := clientset.AdmissionregistrationV1().
			MutatingWebhookConfigurations().
			Get(ctx, configName, metav1.GetOptions{})
		if err != nil {
			return nil, err //nolint:wrapcheck // raw API error needed for IsNotFound classification
		}

		caBundles := make([][]byte, 0, len(cfg.Webhooks))
		for _, webhook := range cfg.Webhooks {
			caBundles = append(caBundles, webhook.ClientConfig.CABundle)
		}

		return caBundles, nil
	}

	cfg, err := clientset.AdmissionregistrationV1().
		ValidatingWebhookConfigurations().
		Get(ctx, configName, metav1.GetOptions{})
	if err != nil {
		return nil, err //nolint:wrapcheck // raw API error needed for IsNotFound classification
	}

	caBundles := make([][]byte, 0, len(cfg.Webhooks))
	for _, webhook := range cfg.Webhooks {
		caBundles = append(caBundles, webhook.ClientConfig.CABundle)
	}

	return caBundles, nil
	// jscpd:ignore-end
}
