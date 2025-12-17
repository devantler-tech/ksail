package installer

import (
	"context"
	"fmt"
	"time"

	"github.com/devantler-tech/ksail-go/pkg/k8s"
	"k8s.io/client-go/kubernetes"
)

// WaitForResourceReadiness waits for multiple Kubernetes resources to become ready.
func WaitForResourceReadiness(
	ctx context.Context,
	kubeconfig, context string,
	checks []k8s.ReadinessCheck,
	timeout time.Duration,
	componentName string,
) error {
	restConfig, err := k8s.BuildRESTConfig(kubeconfig, context)
	if err != nil {
		return fmt.Errorf("build kubernetes client config: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return fmt.Errorf("create kubernetes client: %w", err)
	}

	err = k8s.WaitForMultipleResources(ctx, clientset, checks, timeout)
	if err != nil {
		return fmt.Errorf("wait for %s components: %w", componentName, err)
	}

	return nil
}
