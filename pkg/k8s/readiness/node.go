package readiness

import (
	"context"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// WaitForNodeReady polls until at least one node has condition Ready=True.
// This is used after CNI installation to ensure the network layer is functional
// before proceeding to install post-CNI components.
func WaitForNodeReady(
	ctx context.Context,
	clientset kubernetes.Interface,
	deadline time.Duration,
) error {
	return PollForReadiness(ctx, deadline, func(ctx context.Context) (bool, error) {
		nodes, err := clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
		if err != nil {
			// Continue polling on transient errors
			return false, nil //nolint:nilerr // returning nil to continue polling
		}

		for i := range nodes.Items {
			if isNodeReady(&nodes.Items[i]) {
				return true, nil
			}
		}

		return false, nil
	})
}

// isNodeReady returns true if the node has condition Ready=True.
func isNodeReady(node *corev1.Node) bool {
	for _, cond := range node.Status.Conditions {
		if cond.Type == corev1.NodeReady {
			return cond.Status == corev1.ConditionTrue
		}
	}

	return false
}
