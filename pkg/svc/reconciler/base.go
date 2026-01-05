package reconciler

import (
	"fmt"

	"github.com/devantler-tech/ksail/v5/pkg/k8s"
	"k8s.io/client-go/dynamic"
)

// Base provides shared functionality for GitOps reconcilers.
// It holds a dynamic Kubernetes client that can be used to interact
// with custom resources from different GitOps engines.
type Base struct {
	Dynamic        dynamic.Interface
	KubeconfigPath string
}

// NewBase creates a new Base reconciler with a dynamic client from kubeconfig.
// The dynamic client is configured for the default context in the kubeconfig.
func NewBase(kubeconfigPath string) (*Base, error) {
	restConfig, err := k8s.BuildRESTConfig(kubeconfigPath, "")
	if err != nil {
		return nil, fmt.Errorf("build rest config: %w", err)
	}

	dynamicClient, err := dynamic.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("create dynamic client: %w", err)
	}

	return &Base{
		Dynamic:        dynamicClient,
		KubeconfigPath: kubeconfigPath,
	}, nil
}

// NewBaseWithClient creates a Base with a provided dynamic client (for testing).
func NewBaseWithClient(dynamicClient dynamic.Interface) *Base {
	return &Base{Dynamic: dynamicClient}
}

// New creates a reconciler of type T that embeds Base from a kubeconfig path.
// The constructor function should create the concrete reconciler type with the provided base.
//
//nolint:ireturn // Generic factory function returns interface type by design for dependency injection
func New[T any](kubeconfigPath string, constructor func(*Base) T) (T, error) {
	var zero T

	base, err := NewBase(kubeconfigPath)
	if err != nil {
		return zero, fmt.Errorf("create reconciler base: %w", err)
	}

	return constructor(base), nil
}

// NewWithClient creates a reconciler of type T with a provided dynamic client (for testing).
// The constructor function should create the concrete reconciler type with the provided base.
//
//nolint:ireturn // Generic factory function returns interface type by design for dependency injection
func NewWithClient[T any](dynamicClient dynamic.Interface, constructor func(*Base) T) T {
	return constructor(NewBaseWithClient(dynamicClient))
}
