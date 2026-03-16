package readiness

import (
	"context"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
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
// Returns an error if any DaemonSet is not ready within the deadline. The error
// includes the name and status of the DaemonSet that was blocking readiness.
func WaitForNamespaceDaemonSetsReady(
	ctx context.Context,
	clientset kubernetes.Interface,
	namespace string,
	deadline time.Duration,
) error {
	var lastBlockingDaemonSet string

	pollErr := PollForReadiness(ctx, deadline, func(ctx context.Context) (bool, error) {
		daemonSets, err := clientset.AppsV1().
			DaemonSets(namespace).
			List(ctx, metav1.ListOptions{})
		if err != nil {
			return handleDaemonSetListError(err, namespace)
		}

		if len(daemonSets.Items) == 0 {
			return true, nil
		}

		ready, blocking := checkAllDaemonSetsReady(namespace, daemonSets.Items)
		if blocking != "" {
			lastBlockingDaemonSet = blocking
		}

		return ready, nil
	})

	if pollErr != nil && lastBlockingDaemonSet != "" {
		return fmt.Errorf("%w: blocked by daemonset %s", pollErr, lastBlockingDaemonSet)
	}

	return pollErr
}

// handleDaemonSetListError determines whether a List error is transient
// (and should continue polling) or permanent (and should fail immediately).
func handleDaemonSetListError(err error, namespace string) (bool, error) {
	if apierrors.IsTimeout(err) || apierrors.IsServerTimeout(err) ||
		apierrors.IsTooManyRequests(err) {
		return false, nil
	}

	return false, fmt.Errorf(
		"failed to list daemonsets in namespace %s: %w", namespace, err,
	)
}

// checkAllDaemonSetsReady checks whether every DaemonSet in the slice is ready.
// It returns true if all are ready, and a formatted status string for the first
// non-ready DaemonSet (namespace/name with unavailable, updated, and desired
// counts). Returns an empty string when all DaemonSets are ready.
func checkAllDaemonSetsReady(
	namespace string,
	items []appsv1.DaemonSet,
) (bool, string) {
	for i := range items {
		daemonSet := &items[i]

		// Skip DaemonSets with no desired pods (e.g., node selectors
		// that don't match any nodes). These are not relevant to
		// cluster readiness.
		if daemonSet.Status.DesiredNumberScheduled == 0 {
			continue
		}

		if daemonSet.Status.NumberUnavailable > 0 ||
			daemonSet.Status.UpdatedNumberScheduled != daemonSet.Status.DesiredNumberScheduled {
			return false, fmt.Sprintf(
				"%s/%s (unavailable=%d, updated=%d, desired=%d)",
				namespace, daemonSet.Name,
				daemonSet.Status.NumberUnavailable,
				daemonSet.Status.UpdatedNumberScheduled,
				daemonSet.Status.DesiredNumberScheduled,
			)
		}
	}

	return true, ""
}
