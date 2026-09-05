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

// statefulSetReadyCheck returns a poll function that checks whether a StatefulSet is ready.
// A StatefulSet is considered ready when the controller has observed the current spec, it
// wants at least one replica, every desired replica is Ready, and its rollout is complete.
// NotFound errors are tolerated (returns false to continue polling).
func statefulSetReadyCheck(
	clientset kubernetes.Interface,
	namespace, name string,
) func(context.Context) (bool, error) {
	return func(ctx context.Context) (bool, error) {
		statefulSet, err := clientset.AppsV1().
			StatefulSets(namespace).
			Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				return false, nil
			}

			return false, fmt.Errorf("failed to get statefulset %s/%s: %w", namespace, name, err)
		}

		// The controller must have observed the current spec before its status
		// counters and revisions can be trusted: right after a spec change the
		// Status still describes the previous generation's (possibly ready) pods.
		if statefulSet.Status.ObservedGeneration < statefulSet.Generation {
			return false, nil
		}

		// Compare against the DESIRED count, not Status.Replicas: during a scale-up
		// the controller creates pods one ordinal at a time, so Status.Replicas trails
		// Spec.Replicas and a partially created StatefulSet would otherwise read as
		// ready once its first pod is.
		desired := desiredStatefulSetReplicas(statefulSet)
		if desired == 0 || statefulSet.Status.Replicas == 0 {
			return false, nil
		}

		if statefulSet.Status.ReadyReplicas < desired {
			return false, nil
		}

		return isStatefulSetRolloutComplete(statefulSet, desired), nil
	}
}

// isStatefulSetRolloutComplete reports whether every pod the rollout is meant to
// reach runs the update revision, mirroring `kubectl rollout status`. Under the
// OnDelete strategy the operator moves pods to the new revision by deleting them,
// so revisions are not compared there. A partitioned RollingUpdate deliberately
// leaves the ordinals below the partition on the old revision, so only the pods
// above it are required to be updated.
func isStatefulSetRolloutComplete(statefulSet *appsv1.StatefulSet, desired int32) bool {
	strategy := statefulSet.Spec.UpdateStrategy
	if strategy.Type == appsv1.OnDeleteStatefulSetStrategyType {
		return true
	}

	if strategy.RollingUpdate != nil && strategy.RollingUpdate.Partition != nil &&
		*strategy.RollingUpdate.Partition > 0 {
		return statefulSet.Status.UpdatedReplicas >= desired-*strategy.RollingUpdate.Partition
	}

	return statefulSet.Status.UpdateRevision == statefulSet.Status.CurrentRevision
}

// WaitForStatefulSetReady waits for a StatefulSet to be ready.
//
// This function polls the specified StatefulSet until it is ready or the deadline is reached.
// A StatefulSet is considered ready when:
//   - The controller has observed the current spec
//   - It has at least one replica
//   - All replicas are Ready
//   - Its rollout is complete (see isStatefulSetRolloutComplete)
//
// The function tolerates NotFound errors and continues polling. Other API errors
// are returned immediately.
//
// Returns an error if the StatefulSet is not ready within the deadline or if an API error occurs.
func WaitForStatefulSetReady(
	ctx context.Context,
	clientset kubernetes.Interface,
	namespace, name string,
	deadline time.Duration,
) error {
	return PollForReadiness(ctx, deadline, statefulSetReadyCheck(clientset, namespace, name))
}

// WaitForStatefulSetReadyIfExists waits for a StatefulSet to be ready, but returns
// immediately if the StatefulSet does not exist.
//
// This is useful when a component may or may not be installed, or may be installed
// under another name (e.g. a renamed ArgoCD application controller). If the
// StatefulSet is absent, there is nothing to wait for. If it exists, this function
// waits for it to be fully ready using the same criteria as WaitForStatefulSetReady.
//
// A single deadline bounds the total wall-clock time for both the initial existence
// check and the subsequent readiness polling.
//
// Returns nil if the StatefulSet does not exist (including when the namespace does not exist).
// Returns an error if the StatefulSet exists but is not ready within the deadline.
func WaitForStatefulSetReadyIfExists(
	ctx context.Context,
	clientset kubernetes.Interface,
	namespace, name string,
	deadline time.Duration,
) error {
	lookup := func(lookupCtx context.Context) error {
		_, err := clientset.AppsV1().
			StatefulSets(namespace).
			Get(lookupCtx, name, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("failed to check statefulset %s/%s: %w", namespace, name, err)
		}

		return nil
	}

	return pollIfExists(ctx, deadline, lookup, statefulSetReadyCheck(clientset, namespace, name))
}

// desiredStatefulSetReplicas returns the replica count the StatefulSet is meant
// to reach. Spec.Replicas is a pointer that the API server defaults to 1 when
// unset, so a nil value means one replica, not zero.
func desiredStatefulSetReplicas(statefulSet *appsv1.StatefulSet) int32 {
	if statefulSet.Spec.Replicas == nil {
		return 1
	}

	return *statefulSet.Spec.Replicas
}
