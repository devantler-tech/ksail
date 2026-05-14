package k8s

import (
	"context"
	"fmt"
	"time"

	"k8s.io/client-go/kubernetes"
)

// WaitForAPIServer waits until the Kubernetes API server is reachable and responsive.
// It polls the /readyz endpoint with exponential backoff up to the timeout.
func WaitForAPIServer(kubeconfigPath, contextName string) error {
	restConfig, err := BuildRESTConfig(kubeconfigPath, contextName)
	if err != nil {
		return fmt.Errorf("build REST config: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return fmt.Errorf("create clientset: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	interval := 1 * time.Second

	for {
		body, err := clientset.Discovery().RESTClient().Get().AbsPath("/readyz").DoRaw(ctx)
		if err == nil && string(body) == "ok" {
			return nil
		}

		select {
		case <-ctx.Done():
			if err != nil {
				return fmt.Errorf("API server not ready within timeout: %w", err)
			}

			return fmt.Errorf("API server not ready within timeout (last body: %s)", string(body))
		case <-time.After(interval):
			// Exponential backoff capped at 5s
			interval = min(interval*2, 5*time.Second)
		}
	}
}
