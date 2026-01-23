package fluxinstaller

import (
	"context"
	"fmt"
	"time"

	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// apiWaiter encapsulates logic for waiting on Kubernetes APIs and CRDs to become ready.
// This is necessary because there can be delays between:
// 1. CRD registration and API discovery.
// 2. API discovery and the CRD being fully established.
// 3. CRD establishment and the API being servable.
type apiWaiter struct {
	restConfig *rest.Config
	timeout    time.Duration
	interval   time.Duration
}

// newAPIWaiter creates a new apiWaiter with the given configuration.
func newAPIWaiter(restConfig *rest.Config, timeout, interval time.Duration) *apiWaiter {
	return &apiWaiter{
		restConfig: restConfig,
		timeout:    timeout,
		interval:   interval,
	}
}

// waitForAPIReady waits for both the API to be discoverable and the CRD to be established.
// It also verifies that the resource API is actually servable by attempting to list resources.
func (w *apiWaiter) waitForAPIReady(
	ctx context.Context,
	groupVersion schema.GroupVersion,
	crdName string,
) error {
	// Wait for API to be discoverable
	err := w.waitForGroupVersion(ctx, groupVersion)
	if err != nil {
		return err
	}

	// Wait for CRD to be fully established (not just discoverable)
	// This ensures the API server is ready to accept requests for the resources
	err = w.waitForCRDEstablished(ctx, crdName)
	if err != nil {
		return err
	}

	// Wait for the resource API to be actually servable by the API server.
	// There can be a delay between CRD establishment and the API server
	// discovery endpoint being updated to serve the new resource type.
	return w.waitForResourceAPIServable(ctx, groupVersion)
}

// waitForGroupVersion waits for a GroupVersion to be discoverable via the discovery API.
func (w *apiWaiter) waitForGroupVersion(
	ctx context.Context,
	groupVersion schema.GroupVersion,
) error {
	return pollUntilReady(
		ctx,
		w.timeout,
		w.interval,
		"API "+groupVersion.String(),
		func() (bool, error) {
			discoveryClient, err := newDiscoveryClient(w.restConfig)
			if err != nil {
				return false, fmt.Errorf("failed to create discovery client: %w", err)
			}

			_, err = discoveryClient.ServerResourcesForGroupVersion(groupVersion.String())
			if err != nil {
				return false, fmt.Errorf("API not ready: %w", err)
			}

			return true, nil
		},
	)
}

// waitForCRDEstablished waits for the CRD to be fully established (not just discoverable).
// This ensures the API server is ready to accept requests for the custom resource.
func (w *apiWaiter) waitForCRDEstablished(ctx context.Context, crdName string) error {
	waitCtx, cancel := context.WithTimeout(ctx, w.timeout)
	defer cancel()

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	apiextClient, err := newAPIExtensionsClient(w.restConfig)
	if err != nil {
		return fmt.Errorf("failed to create apiextensions client: %w", err)
	}

	var lastErr error

	for {
		crd := &apiextensionsv1.CustomResourceDefinition{}

		err := apiextClient.Get(waitCtx, client.ObjectKey{Name: crdName}, crd)
		if err != nil {
			lastErr = err
		} else {
			// Check if the CRD has the Established condition set to True
			for _, condition := range crd.Status.Conditions {
				if condition.Type == apiextensionsv1.Established &&
					condition.Status == apiextensionsv1.ConditionTrue {
					return nil
				}
			}

			lastErr = fmt.Errorf("%w: %s", errCRDNotEstablished, crdName)
		}

		select {
		case <-waitCtx.Done():
			if lastErr == nil {
				lastErr = waitCtx.Err()
			}

			return fmt.Errorf(
				"timed out waiting for CRD %s to be established: %w",
				crdName,
				lastErr,
			)
		case <-ticker.C:
		}
	}
}

// waitForResourceAPIServable waits for the resource API to be actually servable.
// This is necessary because there can be a delay between when the CRD controller
// marks a CRD as Established and when the API server's aggregated discovery
// updates to include the new resource type.
func (w *apiWaiter) waitForResourceAPIServable(
	ctx context.Context,
	groupVersion schema.GroupVersion,
) error {
	return pollUntilReady(
		ctx,
		w.timeout,
		w.interval,
		fmt.Sprintf("API %s to be servable", groupVersion.String()),
		func() (bool, error) {
			resources, err := w.tryDiscoverResources(groupVersion)
			if err != nil {
				return false, err
			}

			if resources != nil && len(resources.APIResources) > 0 {
				return true, nil
			}

			return false, fmt.Errorf("%w: %s", errAPINotServable, groupVersion.String())
		},
	)
}

// tryDiscoverResources attempts to discover resources for a group version.
// Returns the resources list or an error if discovery fails.
func (w *apiWaiter) tryDiscoverResources(
	groupVersion schema.GroupVersion,
) (*metav1.APIResourceList, error) {
	// Create a new discovery client on each call to avoid caching issues.
	// The discovery client caches API group information, and a stale cache
	// might not reflect newly-registered resources.
	discoveryClient, err := newDiscoveryClient(w.restConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create discovery client: %w", err)
	}

	// Attempt to get the API resources for the group version.
	// This forces a fresh discovery request and verifies the API is actually servable.
	resources, err := discoveryClient.ServerResourcesForGroupVersion(groupVersion.String())
	if err != nil {
		return nil, fmt.Errorf("failed to get API resources for %s: %w", groupVersion.String(), err)
	}

	return resources, nil
}

// newAPIExtensionsClient creates a client for working with CRDs.
//
//nolint:gochecknoglobals // Allows mocking for tests
var newAPIExtensionsClient = func(restConfig *rest.Config) (client.Client, error) {
	scheme := runtime.NewScheme()

	err := apiextensionsv1.AddToScheme(scheme)
	if err != nil {
		return nil, fmt.Errorf("failed to add apiextensions scheme: %w", err)
	}

	return newDynamicClient(restConfig, scheme)
}

// newDiscoveryClient creates a discovery client for API discovery.
//
//nolint:gochecknoglobals // Allows mocking discovery client for tests
var newDiscoveryClient = func(restConfig *rest.Config) (discovery.DiscoveryInterface, error) {
	return discovery.NewDiscoveryClientForConfig(restConfig)
}
