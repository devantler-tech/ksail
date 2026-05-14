package k8s

import (
	"context"
	"fmt"
	"strings"
	"time"

	"k8s.io/client-go/kubernetes"
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

	waitCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
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
			// Exponential backoff capped at 5s
			interval = min(interval*2, 5*time.Second)
		}
	}
}
