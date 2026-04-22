package readiness

import (
	"context"
	"fmt"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// WaitForDeploymentReady waits for a Deployment to be ready.
//
// This function polls the specified Deployment until it is ready or the deadline is reached.
// A Deployment is considered ready when:
//   - It has at least one replica
//   - All replicas have been updated
//   - All replicas are available
//
// The function tolerates NotFound errors and continues polling. Other API errors
// are returned immediately.
//
// Returns an error if the Deployment is not ready within the deadline or if an API error occurs.
func WaitForDeploymentReady(
	ctx context.Context,
	clientset kubernetes.Interface,
	namespace, name string,
	deadline time.Duration,
) error {
	return PollForReadiness(ctx, deadline, func(ctx context.Context) (bool, error) {
		deployment, err := clientset.AppsV1().
			Deployments(namespace).
			Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				return false, nil
			}

			return false, fmt.Errorf("failed to get deployment %s/%s: %w", namespace, name, err)
		}

		if deployment.Status.Replicas == 0 {
			return false, nil
		}

		if deployment.Status.UpdatedReplicas < deployment.Status.Replicas {
			return false, nil
		}

		if deployment.Status.AvailableReplicas < deployment.Status.Replicas {
			return false, nil
		}

		return true, nil
	})
}

// WaitForDeploymentReadyIfExists waits for a Deployment to be ready, but returns
// immediately if the deployment does not exist.
//
// This is useful when a component may or may not be installed (e.g., kubelet-serving-cert-approver
// via Talos extraManifests). If the deployment is absent, there is nothing to wait for.
// If it exists, this function waits for it to be fully ready using the same criteria
// as WaitForDeploymentReady.
//
// The initial existence check is bounded by the same deadline to prevent indefinite
// blocking if the API server is slow or unresponsive.
//
// Returns nil if the deployment does not exist (including when the namespace does not exist).
// Returns an error if the deployment exists but is not ready within the deadline.
func WaitForDeploymentReadyIfExists(
	ctx context.Context,
	clientset kubernetes.Interface,
	namespace, name string,
	deadline time.Duration,
) error {
	checkCtx, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()

	_, err := clientset.AppsV1().
		Deployments(namespace).
		Get(checkCtx, name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil
		}

		return fmt.Errorf("failed to check deployment %s/%s: %w", namespace, name, err)
	}

	return WaitForDeploymentReady(ctx, clientset, namespace, name, deadline)
}
