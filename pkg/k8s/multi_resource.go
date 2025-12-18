package k8s

import (
	"context"
	"fmt"
	"time"

	"k8s.io/client-go/kubernetes"
)

// ReadinessCheck defines a check to perform for a Kubernetes resource.
type ReadinessCheck struct {
	// Type specifies the kind of Kubernetes resource to check for readiness.
	// Valid values are "deployment" or "daemonset".
	Type string
	// Namespace is the Kubernetes namespace where the resource resides.
	Namespace string
	// Name is the name of the resource to check for readiness.
	Name string
}

// WaitForMultipleResources waits for multiple Kubernetes resources to be ready.
//
// This function checks each resource in sequence, allocating remaining timeout
// proportionally. If any resource fails to become ready within the allocated time,
// the function returns an error.
//
// The timeout parameter is shared across all resources, so each subsequent resource
// gets less time to become ready. Resources are checked in the order they appear
// in the checks slice.
//
// Returns ErrTimeoutExceeded if the timeout is reached before a resource is checked.
// Returns an error if any resource fails to become ready.
func WaitForMultipleResources(
	ctx context.Context,
	clientset kubernetes.Interface,
	checks []ReadinessCheck,
	timeout time.Duration,
) error {
	start := time.Now()

	for _, check := range checks {
		elapsed := time.Since(start)

		remainingTimeout := timeout - elapsed
		if remainingTimeout <= 0 {
			return errTimeoutExceeded(check.Type, check.Namespace, check.Name)
		}

		resourceCtx, cancel := context.WithTimeout(ctx, remainingTimeout)

		var err error

		switch check.Type {
		case "deployment":
			err = WaitForDeploymentReady(
				resourceCtx, clientset, check.Namespace, check.Name, remainingTimeout,
			)
		case "daemonset":
			err = WaitForDaemonSetReady(
				resourceCtx, clientset, check.Namespace, check.Name, remainingTimeout,
			)
		default:
			cancel()

			return fmt.Errorf("%w: %s", errUnknownResourceType, check.Type)
		}

		cancel()

		if err != nil {
			return fmt.Errorf("%s %s not ready: %w", check.Name, check.Type, err)
		}
	}

	return nil
}

// Helper functions.

func errTimeoutExceeded(resourceType, namespace, name string) error {
	return fmt.Errorf(
		"%w before checking %s %s/%s",
		ErrTimeoutExceeded, resourceType, namespace, name,
	)
}
