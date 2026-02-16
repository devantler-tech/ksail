//nolint:err113 // Test helper function uses dynamic errors for type checking
package fluxinstaller

import (
	"context"
	"errors"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Exported functions for testing purposes.
// These wrappers allow the _test package to access internal functions.

// BuildDockerConfigJSON exports buildDockerConfigJSON for testing.
func BuildDockerConfigJSON(registry, username, password string) ([]byte, error) {
	return buildDockerConfigJSON(registry, username, password)
}

// BuildExternalRegistryURL exports buildExternalRegistryURL for testing.
func BuildExternalRegistryURL(localRegistry v1alpha1.LocalRegistry) (string, string, string) {
	return buildExternalRegistryURL(localRegistry)
}

// BuildLocalRegistryURL exports buildLocalRegistryURL for testing.
func BuildLocalRegistryURL(
	localRegistry v1alpha1.LocalRegistry,
	clusterCfg *v1alpha1.Cluster,
	clusterName string,
	registryHostOverride string,
) string {
	return buildLocalRegistryURL(localRegistry, clusterCfg, clusterName, registryHostOverride)
}

// BuildInstance exports buildInstance for testing.
func BuildInstance(
	clusterCfg *v1alpha1.Cluster,
	clusterName string,
	registryHostOverride string,
) (*FluxInstance, error) {
	return buildInstance(clusterCfg, clusterName, registryHostOverride)
}

// BuildRegistrySecret exports buildRegistrySecret for testing.
func BuildRegistrySecret(clusterCfg *v1alpha1.Cluster) (*corev1.Secret, error) {
	return buildRegistrySecret(clusterCfg)
}

// IsTransientAPIError exports isTransientAPIError for testing.
func IsTransientAPIError(err error) bool {
	return isTransientAPIError(err)
}

// NormalizeFluxPath exports normalizeFluxPath for testing.
func NormalizeFluxPath() string {
	return normalizeFluxPath()
}

// PollUntilReady exports pollUntilReady for testing.
func PollUntilReady(
	ctx context.Context,
	timeout time.Duration,
	interval time.Duration,
	resourceDesc string,
	checkFn func() (bool, error),
) error {
	return pollUntilReady(ctx, timeout, interval, resourceDesc, checkFn)
}

// WaitForFluxInstanceReady exports the FluxInstance readiness check for testing.
func WaitForFluxInstanceReady(ctx context.Context, restConfig any) error {
	rc, ok := restConfig.(*rest.Config)
	if !ok {
		return errors.New("invalid rest config type")
	}

	mgr := newFluxInstanceManager(rc, fluxAPIAvailabilityTimeout, fluxAPIAvailabilityPollInterval)

	return mgr.waitForReady(ctx)
}

// ExportNewFluxResourcesClient returns the current newFluxResourcesClient function for testing.
func ExportNewFluxResourcesClient() func(*rest.Config) (any, error) {
	return func(rc *rest.Config) (any, error) {
		return newFluxResourcesClient(rc)
	}
}

// SetNewFluxResourcesClient allows tests to replace newFluxResourcesClient with a mock.
func SetNewFluxResourcesClient(fn func(*rest.Config) (any, error)) func() {
	original := newFluxResourcesClient
	newFluxResourcesClient = func(rc *rest.Config) (client.Client, error) {
		c, err := fn(rc)
		if err != nil {
			return nil, err
		}

		client, ok := c.(client.Client)
		if !ok {
			return nil, errors.New("invalid client type")
		}

		return client, nil
	}

	return func() {
		newFluxResourcesClient = original
	}
}
