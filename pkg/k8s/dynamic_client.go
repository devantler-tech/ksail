package k8s

import (
	"fmt"

	"k8s.io/client-go/dynamic"
)

// NewDynamicClient creates a Kubernetes dynamic client from kubeconfig path and context.
// This is a convenience function that combines BuildRESTConfig and dynamic client creation.
// Use this when working with unstructured resources or custom resource types.
func NewDynamicClient(kubeconfig, context string) (dynamic.Interface, error) {
	restConfig, err := BuildRESTConfig(kubeconfig, context)
	if err != nil {
		return nil, fmt.Errorf("failed to build rest config: %w", err)
	}

	client, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create dynamic client: %w", err)
	}

	return client, nil
}
