package fluxinstaller

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/k8s"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
)

// Error definitions for the flux installer package.
var (
	errCRDNotEstablished         = errors.New("CRD is not yet established")
	errAPINotServable            = errors.New("API returned no resources")
	errOCIRepositoryCreateTimout = errors.New("timed out waiting for OCIRepository to be created")
	errPollTimeout               = errors.New("timed out waiting for resource to be ready")
	errInvalidClusterConfig      = errors.New("cluster configuration is required")
)

// CRD names for waiting on establishment.
const (
	fluxInstanceCRDName = "fluxinstances.fluxcd.controlplane.io"
)

// Timing constants for API availability checks.
const (
	// apiStabilizationDelay is a pause after the API is reported ready,
	// allowing the API server to fully propagate the CRD to all endpoints.
	// This addresses race conditions observed in slower CI environments (e.g., Talos on GitHub Actions)
	// where discovery reports the API as ready slightly before Create operations can succeed.
	// 10 seconds has been empirically determined to provide sufficient margin for GitHub Actions runners.
	apiStabilizationDelay = 10 * time.Second
)

//nolint:gochecknoglobals // package-level timeout constants
var (
	// fluxAPIAvailabilityTimeout is the maximum time to wait for Flux CRDs to become available.
	// This timeout should balance quick feedback for errors with enough time for slower
	// environments like Talos on GitHub Actions. 5 minutes is typically sufficient.
	fluxAPIAvailabilityTimeout      = 5 * time.Minute
	fluxAPIAvailabilityPollInterval = 2 * time.Second
)

// loadRESTConfig creates a REST config from a kubeconfig path.
//
//nolint:gochecknoglobals // Allows mocking REST config for tests
var loadRESTConfig = func(kubeconfig string) (*rest.Config, error) {
	return k8s.BuildRESTConfig(kubeconfig, "")
}

// setupParams holds the components needed for Flux setup operations.
// Context is passed separately to setupFluxCore to avoid embedding it in a struct.
type setupParams struct {
	restConfig  *rest.Config
	clusterCfg  *v1alpha1.Cluster
	clusterName string
}

// setupFluxCore performs the common Flux setup: secret creation, FluxInstance creation, and OCIRepository patching.
func setupFluxCore(ctx context.Context, params setupParams) error {
	// For external registries with credentials, create the pull secret before FluxInstance
	err := ensureExternalRegistrySecret(ctx, params.restConfig, params.clusterCfg)
	if err != nil {
		return err
	}

	// Setup FluxInstance
	fluxMgr := newFluxInstanceManager(
		params.restConfig,
		fluxAPIAvailabilityTimeout,
		fluxAPIAvailabilityPollInterval,
	)

	err = fluxMgr.setup(ctx, params.clusterCfg, params.clusterName)
	if err != nil {
		return err
	}

	// Wait for OCIRepository API to be available before patching
	ociPatcher := newOCIRepositoryPatcher(
		params.restConfig,
		fluxAPIAvailabilityTimeout,
		fluxAPIAvailabilityPollInterval,
	)

	err = ociPatcher.waitForAPI(ctx)
	if err != nil {
		return err
	}

	// For local Docker registries (not external like GHCR), patch OCIRepository to use insecure HTTP
	return ensureLocalRegistryInsecureIfNeeded(ctx, ociPatcher, params.clusterCfg)
}

// EnsureDefaultResources configures a default FluxInstance so the operator can
// bootstrap controllers and sync from the local OCI registry.
// If artifactPushed is false, the function will skip waiting for FluxInstance readiness
// because the artifact doesn't exist yet (will be pushed later via workload push).
//
//nolint:contextcheck // context passed from caller and used in nested functions
func EnsureDefaultResources(
	ctx context.Context,
	kubeconfig string,
	clusterCfg *v1alpha1.Cluster,
	clusterName string,
	artifactPushed bool,
) error {
	if clusterCfg == nil {
		return errInvalidClusterConfig
	}

	if ctx == nil {
		ctx = context.Background()
	}

	restConfig, err := loadRESTConfig(kubeconfig)
	if err != nil {
		return err
	}

	err = setupFluxCore(ctx, setupParams{
		restConfig:  restConfig,
		clusterCfg:  clusterCfg,
		clusterName: clusterName,
	})
	if err != nil {
		return err
	}

	// Only wait for FluxInstance readiness if artifact was pushed.
	// If no artifact was pushed (e.g., source directory missing during cluster create),
	// the FluxInstance will remain in "Reconciliation in progress" until workload push is run.
	if artifactPushed {
		fluxMgr := newFluxInstanceManager(
			restConfig,
			fluxAPIAvailabilityTimeout,
			fluxAPIAvailabilityPollInterval,
		)

		err = fluxMgr.waitForReady(ctx)
		if err != nil {
			return fmt.Errorf("failed waiting for FluxInstance to be ready: %w", err)
		}
	}

	return nil
}

// SetupInstance creates the FluxInstance CR and configures OCIRepository settings.
// This does NOT wait for FluxInstance to be ready - use WaitForFluxReady after pushing artifacts.
// Returns error if setup fails.
//
//nolint:contextcheck // context passed from caller and used in nested functions
func SetupInstance(
	ctx context.Context,
	kubeconfig string,
	clusterCfg *v1alpha1.Cluster,
	clusterName string,
) error {
	if clusterCfg == nil {
		return errInvalidClusterConfig
	}

	if ctx == nil {
		ctx = context.Background()
	}

	restConfig, err := loadRESTConfig(kubeconfig)
	if err != nil {
		return err
	}

	return setupFluxCore(ctx, setupParams{
		restConfig:  restConfig,
		clusterCfg:  clusterCfg,
		clusterName: clusterName,
	})
}

// WaitForFluxReady waits for the FluxInstance to report a Ready condition.
// Call this after pushing an OCI artifact to ensure Flux has successfully reconciled.
//
//nolint:contextcheck // context passed from caller
func WaitForFluxReady(
	ctx context.Context,
	kubeconfig string,
) error {
	if ctx == nil {
		ctx = context.Background()
	}

	restConfig, err := loadRESTConfig(kubeconfig)
	if err != nil {
		return err
	}

	fluxMgr := newFluxInstanceManager(
		restConfig,
		fluxAPIAvailabilityTimeout,
		fluxAPIAvailabilityPollInterval,
	)

	err = fluxMgr.waitForReady(ctx)
	if err != nil {
		return fmt.Errorf("failed waiting for FluxInstance to be ready: %w", err)
	}

	return nil
}

// ensureLocalRegistryInsecureIfNeeded patches OCIRepository with insecure: true only for
// local Docker registries. External registries like GHCR use HTTPS and should not be patched.
func ensureLocalRegistryInsecureIfNeeded(
	ctx context.Context,
	patcher *ociRepositoryPatcher,
	clusterCfg *v1alpha1.Cluster,
) error {
	localRegistry := clusterCfg.Spec.Cluster.LocalRegistry
	if !localRegistry.Enabled() || localRegistry.IsExternal() {
		return nil
	}

	return patcher.ensureInsecure(ctx)
}

// newDynamicClient creates a controller-runtime client with a dynamic REST mapper.
// The dynamic mapper re-discovers resources on cache misses, which is critical for
// newly-registered CRDs where a static mapper might have stale cached data.
func newDynamicClient(restConfig *rest.Config, scheme *runtime.Scheme) (client.Client, error) {
	httpClient, err := rest.HTTPClientFor(restConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP client: %w", err)
	}

	mapper, err := apiutil.NewDynamicRESTMapper(restConfig, httpClient)
	if err != nil {
		return nil, fmt.Errorf("failed to create dynamic REST mapper: %w", err)
	}

	k8sClient, err := client.New(restConfig, client.Options{
		Scheme: scheme,
		Mapper: mapper,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	return k8sClient, nil
}

// pollUntilReady implements a generic polling pattern for waiting on async conditions.
// It repeatedly calls checkFn until it returns true (success) or the context expires.
func pollUntilReady(
	ctx context.Context,
	timeout time.Duration,
	interval time.Duration,
	resourceDesc string,
	checkFn func() (bool, error),
) error {
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var lastErr error

	for {
		ready, err := checkFn()
		if err != nil {
			// Store transient errors for timeout reporting
			lastErr = err
		}

		if ready {
			return nil
		}

		select {
		case <-waitCtx.Done():
			if lastErr == nil {
				lastErr = waitCtx.Err()
			}

			return fmt.Errorf("%w: %s: %w", errPollTimeout, resourceDesc, lastErr)
		case <-ticker.C:
		}
	}
}

// isTransientAPIError checks if the error is a transient API error that should be retried.
func isTransientAPIError(err error) bool {
	if err == nil {
		return false
	}

	// Check for specific Kubernetes API errors
	if apierrors.IsServiceUnavailable(err) ||
		apierrors.IsTimeout(err) ||
		apierrors.IsTooManyRequests(err) ||
		apierrors.IsConflict(err) {
		return true
	}

	errMsg := err.Error()

	// Known transient error patterns
	transientPatterns := []string{
		"the server could not find the requested resource",
		"no matches for kind",
		"connection refused",
		"connection reset",
	}

	for _, pattern := range transientPatterns {
		if strings.Contains(errMsg, pattern) {
			return true
		}
	}

	return false
}
