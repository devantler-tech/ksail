package k8s

import (
	"context"

	"k8s.io/client-go/kubernetes"
)

// WaitForAuthorizedReadForTest exposes waitForAuthorizedRead for unit testing
// with a fake clientset, avoiding the need for a real API server.
func WaitForAuthorizedReadForTest(ctx context.Context, clientset kubernetes.Interface) error {
	return waitForAuthorizedRead(ctx, clientset)
}
