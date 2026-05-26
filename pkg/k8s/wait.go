package k8s

import (
	"context"
	"fmt"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	// waitForAPIServerTimeout is the maximum time to wait for the API server to become ready.
	waitForAPIServerTimeout = 60 * time.Second

	// waitForAuthorizedReadTimeout is the maximum time to wait for a basic
	// authorized read to succeed after the API server reports ready. This
	// covers the authorizer warm-up window where the API server is reachable
	// but RBAC checks transiently return 403, and the brief delay before
	// built-in resources (e.g. the default namespace's ServiceAccount) are
	// reconciled.
	waitForAuthorizedReadTimeout = 60 * time.Second

	// waitBackoffMultiplier is the exponential backoff multiplier for the wait interval.
	waitBackoffMultiplier = 2

	// maxWaitInterval is the maximum backoff interval between API server readiness polls.
	maxWaitInterval = 5 * time.Second
)

// WaitForAPIServer waits until the Kubernetes API server is reachable and responsive.
// It polls the /readyz endpoint with exponential backoff up to the timeout.
func WaitForAPIServer(ctx context.Context, kubeconfigPath, contextName string) error {
	restConfig, err := BuildRESTConfig(kubeconfigPath, contextName)
	if err != nil {
		return fmt.Errorf("build REST config: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return fmt.Errorf("create clientset: %w", err)
	}

	waitCtx, cancel := context.WithTimeout(ctx, waitForAPIServerTimeout)
	defer cancel()

	interval := 1 * time.Second

	for {
		body, err := clientset.Discovery().RESTClient().Get().AbsPath("/readyz").DoRaw(waitCtx)
		if err == nil && strings.TrimSpace(string(body)) == "ok" {
			return nil
		}

		select {
		case <-waitCtx.Done():
			if err != nil {
				return fmt.Errorf("%w: %w", ErrAPIServerTimeout, err)
			}

			return fmt.Errorf("%w (last body: %s)", ErrAPIServerTimeout, string(body))
		case <-time.After(interval):
			// Exponential backoff capped at maxWaitInterval
			interval = min(interval*waitBackoffMultiplier, maxWaitInterval)
		}
	}
}

// WaitForClusterReady waits until the cluster is genuinely ready for use: the
// API server is reachable (/readyz returns "ok") AND a basic authorized read
// (listing namespaces) succeeds.
//
// The authorized-read step exists because the API server can report ready
// while the authorizer is still warming up — transiently returning 403
// ("... cannot list namespaces") — and before built-in resources such as the
// default namespace's ServiceAccount have been reconciled. Distributions whose
// Start() returns as soon as the node container is up (Kind, K3d) call this so
// callers get a usable cluster instead of racing the warm-up window.
func WaitForClusterReady(ctx context.Context, kubeconfigPath, contextName string) error {
	resolvedPath, err := ResolveKubeconfigPath(kubeconfigPath)
	if err != nil {
		return fmt.Errorf("resolve kubeconfig path: %w", err)
	}

	err = WaitForAPIServer(ctx, resolvedPath, contextName)
	if err != nil {
		return err
	}

	clientset, err := NewClientset(resolvedPath, contextName)
	if err != nil {
		return fmt.Errorf("create clientset: %w", err)
	}

	return waitForAuthorizedRead(ctx, clientset)
}

// waitForAuthorizedRead polls a minimal authorized read (listing namespaces)
// until it succeeds or the timeout elapses. Any error — including a transient
// 403 from the warming-up authorizer — is treated as "not ready yet" and
// retried, since the credentials come from the cluster's own kubeconfig and a
// real authorization failure is not expected here.
func waitForAuthorizedRead(ctx context.Context, clientset kubernetes.Interface) error {
	waitCtx, cancel := context.WithTimeout(ctx, waitForAuthorizedReadTimeout)
	defer cancel()

	interval := 1 * time.Second

	for {
		_, err := clientset.CoreV1().Namespaces().List(waitCtx, metav1.ListOptions{Limit: 1})
		if err == nil {
			return nil
		}

		select {
		case <-waitCtx.Done():
			return fmt.Errorf("%w: %w", ErrClusterNotReady, err)
		case <-time.After(interval):
			// Exponential backoff capped at maxWaitInterval
			interval = min(interval*waitBackoffMultiplier, maxWaitInterval)
		}
	}
}
