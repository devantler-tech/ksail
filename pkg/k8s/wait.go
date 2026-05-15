package k8s

import (
	"context"
	"fmt"
	"strings"
	"time"

	"k8s.io/client-go/kubernetes"
)

const (
	// waitForAPIServerTimeout is the maximum time to wait for the API server to become ready.
	waitForAPIServerTimeout = 60 * time.Second

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
