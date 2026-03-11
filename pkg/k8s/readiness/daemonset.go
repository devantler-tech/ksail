package readiness

import (
	"context"
	"fmt"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// WaitForDaemonSetReady waits for a DaemonSet to be ready.
//
// This function polls the specified DaemonSet until it is ready or the deadline is reached.
// A DaemonSet is considered ready when:
//   - At least one pod is scheduled
//   - No pods are unavailable
//   - All pods have been updated to the current specification
//
// The function tolerates NotFound errors and continues polling. Other API errors
// are returned immediately.
//
// Returns an error if the DaemonSet is not ready within the deadline or if an API error occurs.
func WaitForDaemonSetReady(
	ctx context.Context,
	clientset kubernetes.Interface,
	namespace, name string,
	deadline time.Duration,
) error {
	return PollForReadiness(ctx, deadline, func(ctx context.Context) (bool, error) {
		daemonSet, err := clientset.AppsV1().
			DaemonSets(namespace).
			Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				return false, nil
			}

			return false, fmt.Errorf("failed to get daemonset %s/%s: %w", namespace, name, err)
		}

		if daemonSet.Status.DesiredNumberScheduled == 0 {
			return false, nil
		}

		ready := daemonSet.Status.NumberUnavailable == 0 &&
			daemonSet.Status.UpdatedNumberScheduled == daemonSet.Status.DesiredNumberScheduled

		return ready, nil
	})
}

// WaitForNamespaceDaemonSetsReady waits for all DaemonSets in a namespace to be ready.
//
// This is particularly useful after infrastructure component installations to ensure
// that CNI DaemonSets (e.g., Cilium) have fully re-converged. Cilium marks its pods
// as Ready only after the BPF datapath is operational, so waiting for all kube-system
// DaemonSets ensures that pod-to-service routing is functional before starting
// workloads that depend on in-cluster API server connectivity.
//
// Returns nil if the namespace has no DaemonSets.
// Returns an error if any DaemonSet is not ready within the deadline.
func WaitForNamespaceDaemonSetsReady(
	ctx context.Context,
	clientset kubernetes.Interface,
	namespace string,
	deadline time.Duration,
) error {
	return PollForReadiness(ctx, deadline, func(ctx context.Context) (bool, error) {
		daemonSets, err := clientset.AppsV1().
			DaemonSets(namespace).
			List(ctx, metav1.ListOptions{})
		if err != nil {
			// Continue polling on transient errors
			return false, nil //nolint:nilerr // returning nil to continue polling
		}

		if len(daemonSets.Items) == 0 {
			return true, nil
		}

		for i := range daemonSets.Items {
			ds := &daemonSets.Items[i]

			if ds.Status.DesiredNumberScheduled == 0 {
				return false, nil
			}

			if ds.Status.NumberUnavailable > 0 {
				return false, nil
			}

			if ds.Status.UpdatedNumberScheduled != ds.Status.DesiredNumberScheduled {
				return false, nil
			}
		}

		return true, nil
	})
}
