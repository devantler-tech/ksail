//nolint:err113 // Test helper function uses dynamic errors for type checking
package fluxinstaller

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	fluxclient "github.com/devantler-tech/ksail/v7/pkg/client/flux"
	"github.com/devantler-tech/ksail/v7/pkg/svc/installer/internal/sopsutil"
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

// NormalizeFluxPath exports normalizeFluxPath(kustomizationFile) for testing.
// kustomizationFile is the path to the Flux kustomization entry point directory
// that is normalized into the Flux Sync.Path used by the installer.
func NormalizeFluxPath(kustomizationFile string) string {
	return normalizeFluxPath(kustomizationFile)
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

// SetLoadRESTConfig allows tests to replace loadRESTConfig with a stub.
func SetLoadRESTConfig(fn func(string) (*rest.Config, error)) func() {
	original := loadRESTConfig
	loadRESTConfig = fn

	return func() {
		loadRESTConfig = original
	}
}

// SetDiagnoseFluxPodFailures allows tests to replace the pod diagnostics function with a stub.
func SetDiagnoseFluxPodFailures(fn func(context.Context, *rest.Config) string) func() {
	original := diagnoseFluxPodFailures
	diagnoseFluxPodFailures = fn

	return func() {
		diagnoseFluxPodFailures = original
	}
}

// SetSetupFluxCoreToNoop replaces the core Flux setup function with a no-op stub
// that always returns nil, allowing tests to skip API server and CRD interactions.
func SetSetupFluxCoreToNoop() func() {
	original := setupFluxCoreImpl
	setupFluxCoreImpl = func(_ context.Context, _ setupParams) error { return nil }

	return func() {
		setupFluxCoreImpl = original
	}
}

func ResolveAgeKey(sops v1alpha1.SOPS) (string, error) {
	key, err := sopsutil.ResolveAgeKey(sops)
	if err != nil {
		return "", fmt.Errorf("resolve age key: %w", err)
	}

	return key, nil
}

// ExtractAgeKey exports sopsutil.ExtractAgeKey for testing.
func ExtractAgeKey(input string) string {
	return sopsutil.ExtractAgeKey(input)
}

// BuildSopsAgeSecret exports sopsutil.BuildSopsAgeSecret for testing with the flux-system namespace.
func BuildSopsAgeSecret(ageKey string) *corev1.Secret {
	return sopsutil.BuildSopsAgeSecret(fluxclient.DefaultNamespace, ageKey)
}

// EnsureSopsAgeSecret exports ensureSopsAgeSecret for testing.
func EnsureSopsAgeSecret(
	ctx context.Context,
	restConfig *rest.Config,
	clusterCfg *v1alpha1.Cluster,
) error {
	return ensureSopsAgeSecret(ctx, restConfig, clusterCfg)
}
