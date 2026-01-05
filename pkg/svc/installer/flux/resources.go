package fluxinstaller

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	dockerclient "github.com/devantler-tech/ksail/v5/pkg/client/docker"
	fluxclient "github.com/devantler-tech/ksail/v5/pkg/client/flux"
	"github.com/devantler-tech/ksail/v5/pkg/k8s"
	registry "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/registry"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
)

const (
	defaultSourceDirectory   = "k8s"
	defaultArtifactTag       = "dev"
	defaultOCIRepositoryName = fluxclient.DefaultNamespace
	fluxIntervalFallback     = time.Minute
	fluxDistributionVersion  = "2.x"
	fluxDistributionRegistry = "ghcr.io/fluxcd"
	fluxDistributionArtifact = "oci://ghcr.io/controlplaneio-fluxcd/flux-operator-manifests:latest"
	// CRD names for waiting on establishment.
	fluxInstanceCRDName    = "fluxinstances.fluxcd.controlplane.io"
	ociRepositoriesCRDName = "ocirepositories.source.toolkit.fluxcd.io"
	// apiStabilizationDelay is a pause after the API is reported ready,
	// allowing the API server to fully propagate the CRD to all endpoints.
	// This addresses race conditions observed in slower CI environments (e.g., Talos on GitHub Actions)
	// where discovery reports the API as ready slightly before Create operations can succeed.
	// 10 seconds has been empirically determined to provide sufficient margin for GitHub Actions runners.
	apiStabilizationDelay = 10 * time.Second
)

var (
	errCRDNotEstablished         = errors.New("CRD is not yet established")
	errAPINotServable            = errors.New("API returned no resources")
	errOCIRepositoryCreateTimout = errors.New("timed out waiting for OCIRepository to be created")
)

//nolint:gochecknoglobals // package-level timeout constants
var (
	// fluxAPIAvailabilityTimeout is the maximum time to wait for Flux CRDs to become available.
	// This timeout is intentionally long (10 minutes) to accommodate slower environments like
	// Talos on GitHub Actions, where the FluxInstance operator needs to:
	// 1. Pull controller images from ghcr.io/fluxcd (can take 3-5+ minutes in nested containers)
	// 2. Deploy the source-controller
	// 3. Wait for source-controller pod to be ready and register the OCIRepository CRD
	// In fast environments (Kind), this typically completes in seconds.
	// Empirically, Talos on GitHub Actions requires 5-8 minutes for this process.
	fluxAPIAvailabilityTimeout      = 10 * time.Minute
	fluxAPIAvailabilityPollInterval = 2 * time.Second
)

var (
	errInvalidClusterConfig = errors.New("cluster configuration is required")

	//nolint:gochecknoglobals // Allows mocking REST config for tests
	loadRESTConfig = func(kubeconfig string) (*rest.Config, error) {
		return k8s.BuildRESTConfig(kubeconfig, "")
	}

	//nolint:noinlineerr,gochecknoglobals // error handling in scheme registration, allows mocking for tests
	newFluxResourcesClient = func(restConfig *rest.Config) (client.Client, error) {
		scheme := runtime.NewScheme()

		if err := addFluxInstanceToScheme(scheme); err != nil {
			return nil, fmt.Errorf("failed to add flux instance scheme: %w", err)
		}

		if err := sourcev1.AddToScheme(scheme); err != nil {
			return nil, fmt.Errorf("failed to add flux source scheme: %w", err)
		}

		return newDynamicClient(restConfig, scheme)
	}

	//nolint:gochecknoglobals // Allows mocking discovery client for tests
	newDiscoveryClient = func(restConfig *rest.Config) (discovery.DiscoveryInterface, error) {
		return discovery.NewDiscoveryClientForConfig(restConfig)
	}

	//nolint:gochecknoglobals // Allows mocking for tests
	newAPIExtensionsClient = func(restConfig *rest.Config) (client.Client, error) {
		scheme := runtime.NewScheme()

		err := apiextensionsv1.AddToScheme(scheme)
		if err != nil {
			return nil, fmt.Errorf("failed to add apiextensions scheme: %w", err)
		}

		return newDynamicClient(restConfig, scheme)
	}
)

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

// EnsureDefaultResources configures a default FluxInstance so the operator can
// bootstrap controllers and sync from the local OCI registry.
//
//nolint:contextcheck // context passed from caller and used in nested functions
func EnsureDefaultResources(
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

	_, err = setupFluxInstance(ctx, restConfig, clusterCfg, clusterName)
	if err != nil {
		return err
	}

	// Wait for OCIRepository API to be available before patching the local registry.
	// The FluxInstance reconciliation will create the OCIRepository CRD and resources,
	// but the FluxInstance won't become Ready until the OCIRepository can sync.
	// For local registries, we need to patch the OCIRepository to use insecure HTTP
	// before the sync can succeed.
	err = waitForAPIReady(ctx, restConfig, sourcev1.GroupVersion, ociRepositoriesCRDName)
	if err != nil {
		return err
	}

	// Brief stabilization delay for OCIRepository API as well
	select {
	case <-ctx.Done():
		return fmt.Errorf("context cancelled during OCIRepository API stabilization: %w", ctx.Err())
	case <-time.After(apiStabilizationDelay):
	}

	if clusterCfg.Spec.Cluster.LocalRegistry == v1alpha1.LocalRegistryEnabled {
		// Use dynamic client approach for OCIRepository operations.
		// This bypasses the controller-runtime REST mapper cache which can become
		// stale in slow CI environments where CRD registration takes time to propagate.
		err = ensureLocalOCIRepositoryInsecure(ctx, restConfig)
		if err != nil {
			return err
		}
	}

	// Note: We don't wait for FluxInstance to be Ready here because it depends on
	// the OCIRepository sync, which requires the workload to be pushed first.
	// The workload push happens after cluster creation via 'ksail workload push'.
	return nil
}

// setupFluxInstance waits for the FluxInstance CRD, creates the client, and upserts the FluxInstance.
func setupFluxInstance(
	ctx context.Context,
	restConfig *rest.Config,
	clusterCfg *v1alpha1.Cluster,
	clusterName string,
) (client.Client, error) {
	// Wait for FluxInstance API to be fully ready
	err := waitForAPIReady(ctx, restConfig, fluxInstanceGroupVersion, fluxInstanceCRDName)
	if err != nil {
		return nil, err
	}

	// Brief stabilization delay to allow the API server to fully propagate the CRD
	// across all its endpoints. This addresses race conditions observed in slower
	// CI environments (e.g., Talos on GitHub Actions) where discovery reports the
	// API as ready slightly before Create operations can succeed.
	select {
	case <-ctx.Done():
		return nil, fmt.Errorf(
			"context cancelled during FluxInstance API stabilization: %w",
			ctx.Err(),
		)
	case <-time.After(apiStabilizationDelay):
	}

	fluxInstance, err := buildFluxInstance(clusterCfg, clusterName)
	if err != nil {
		return nil, err
	}

	// Create a client factory that creates a fresh client on each retry.
	// This is necessary because the dynamic REST mapper caches discovery results,
	// and if the initial discovery happens before the API server fully propagates
	// the CRD, subsequent requests will fail until the cache expires.
	clientFactory := func() (client.Client, error) {
		return newFluxResourcesClient(restConfig)
	}

	fluxClient, err := upsertFluxInstanceWithRetry(ctx, clientFactory, fluxInstance)
	if err != nil {
		return nil, err
	}

	return fluxClient, nil
}

// waitForAPIReady waits for both the API to be discoverable and the CRD to be established.
// It also verifies that the resource API is actually servable by attempting to list resources.
func waitForAPIReady(
	ctx context.Context,
	restConfig *rest.Config,
	groupVersion schema.GroupVersion,
	crdName string,
) error {
	// Wait for API to be discoverable
	err := waitForGroupVersion(ctx, restConfig, groupVersion)
	if err != nil {
		return err
	}

	// Wait for CRD to be fully established (not just discoverable)
	// This ensures the API server is ready to accept requests for the resources
	err = waitForCRDEstablished(ctx, restConfig, crdName)
	if err != nil {
		return err
	}

	// Wait for the resource API to be actually servable by the API server.
	// There can be a delay between CRD establishment and the API server
	// discovery endpoint being updated to serve the new resource type.
	return waitForResourceAPIServable(ctx, restConfig, groupVersion)
}

//nolint:unparam // error return kept for consistency with resource building patterns
func buildFluxInstance(clusterCfg *v1alpha1.Cluster, clusterName string) (*FluxInstance, error) {
	// Use the fallback interval value. Interval configuration is now managed
	// via the FluxInstance CR in the source directory, not in the KSail config.
	interval := fluxIntervalFallback

	hostPort := clusterCfg.Spec.Cluster.LocalRegistryOpts.HostPort
	if hostPort == 0 {
		hostPort = v1alpha1.DefaultLocalRegistryPort
	}

	sourceDir := strings.TrimSpace(clusterCfg.Spec.Workload.SourceDirectory)
	if sourceDir == "" {
		sourceDir = defaultSourceDirectory
	}

	projectName := registry.SanitizeRepoName(sourceDir)
	// Build the cluster-prefixed local registry name for in-cluster DNS resolution
	repoHost := registry.BuildLocalRegistryName(clusterName)
	repoPort := dockerclient.DefaultRegistryPort

	if clusterCfg.Spec.Cluster.LocalRegistry != v1alpha1.LocalRegistryEnabled {
		repoHost = registry.DefaultEndpointHost
		repoPort = int(hostPort)
	}

	repoURL := fmt.Sprintf(
		"oci://%s/%s",
		net.JoinHostPort(repoHost, strconv.Itoa(repoPort)),
		projectName,
	)
	normalizedPath := normalizeFluxPath()
	intervalPtr := &metav1.Duration{Duration: interval}

	return &FluxInstance{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fluxInstanceDefaultName,
			Namespace: fluxclient.DefaultNamespace,
		},
		Spec: FluxInstanceSpec{
			Distribution: Distribution{
				Version:  fluxDistributionVersion,
				Registry: fluxDistributionRegistry,
				Artifact: fluxDistributionArtifact,
			},
			Sync: &Sync{
				Kind:     fluxOCIRepositoryKind,
				URL:      repoURL,
				Ref:      defaultArtifactTag,
				Path:     normalizedPath,
				Provider: "generic",
				Interval: intervalPtr,
			},
		},
	}, nil
}

// upsertFluxInstanceWithRetry creates or updates a FluxInstance with retry logic
// to handle transient API errors during CRD initialization.
// It accepts a client factory to create a fresh client on each retry, which is
// necessary because the dynamic REST mapper caches discovery results.
func upsertFluxInstanceWithRetry(
	ctx context.Context,
	clientFactory func() (client.Client, error),
	desired *FluxInstance,
) (client.Client, error) {
	waitCtx, cancel := context.WithTimeout(ctx, fluxAPIAvailabilityTimeout)
	defer cancel()

	ticker := time.NewTicker(fluxAPIAvailabilityPollInterval)
	defer ticker.Stop()

	key := client.ObjectKeyFromObject(desired)

	var lastErr error

	for {
		// Create a fresh client on each retry to ensure the dynamic REST mapper
		// picks up newly-registered CRDs that might not have been discoverable
		// in previous attempts.
		fluxClient, clientErr := clientFactory()
		if clientErr != nil {
			lastErr = clientErr

			select {
			case <-waitCtx.Done():
				return nil, fmt.Errorf(
					"timed out creating client for FluxInstance %s/%s: %w",
					key.Namespace,
					key.Name,
					lastErr,
				)
			case <-ticker.C:
				continue
			}
		}

		err := tryUpsertFluxInstance(waitCtx, fluxClient, key, desired)
		if err == nil {
			return fluxClient, nil
		}

		// If the error is a transient API error (like "resource not found" during CRD init),
		// retry. Otherwise, return the error immediately.
		if !isTransientAPIError(err) {
			return nil, err
		}

		lastErr = err

		select {
		case <-waitCtx.Done():
			if lastErr == nil {
				lastErr = waitCtx.Err()
			}

			return nil, fmt.Errorf(
				"timed out upserting FluxInstance %s/%s: %w",
				key.Namespace,
				key.Name,
				lastErr,
			)
		case <-ticker.C:
			// Retry with a fresh client
		}
	}
}

// tryUpsertFluxInstance attempts to create or update a FluxInstance once.
func tryUpsertFluxInstance(
	ctx context.Context,
	fluxClient client.Client,
	key client.ObjectKey,
	desired *FluxInstance,
) error {
	existing := &FluxInstance{}

	err := fluxClient.Get(ctx, key, existing)
	if err != nil {
		if apierrors.IsNotFound(err) {
			createErr := fluxClient.Create(ctx, desired)
			if createErr != nil {
				return fmt.Errorf(
					"create FluxInstance %s/%s: %w",
					key.Namespace,
					key.Name,
					createErr,
				)
			}

			return nil
		}

		return fmt.Errorf("failed to get FluxInstance %s/%s: %w", key.Namespace, key.Name, err)
	}

	existing.Spec = desired.Spec

	err = fluxClient.Update(ctx, existing)
	if err != nil {
		return fmt.Errorf("failed to update FluxInstance %s/%s: %w", key.Namespace, key.Name, err)
	}

	return nil
}

// isTransientAPIError checks if the error is a transient API error that should be retried.
// This includes errors like "the server could not find the requested resource" which can
// occur when a CRD is registered but not fully ready to accept requests.
func isTransientAPIError(err error) bool {
	if err == nil {
		return false
	}

	// Check for specific status errors that indicate the API isn't ready
	if apierrors.IsServiceUnavailable(err) {
		return true
	}

	// Check for connection-related errors that can occur during API server restarts
	if apierrors.IsTimeout(err) || apierrors.IsTooManyRequests(err) {
		return true
	}

	// String-based checks for errors that aren't properly typed
	errMsg := err.Error()

	// "the server could not find the requested resource" indicates the CRD endpoint
	// isn't fully registered yet
	if strings.Contains(errMsg, "the server could not find the requested resource") {
		return true
	}

	// "no matches for kind" is a REST mapper error when the CRD isn't known yet
	if strings.Contains(errMsg, "no matches for kind") {
		return true
	}

	// Connection refused/reset can happen during API server initialization
	if strings.Contains(errMsg, "connection refused") ||
		strings.Contains(errMsg, "connection reset") {
		return true
	}

	return false
}

// ensureLocalOCIRepositoryInsecure uses a dynamic Kubernetes client to patch the OCIRepository
// to enable insecure HTTP access for local registries. Using the dynamic client with
// unstructured objects bypasses the controller-runtime REST mapper cache, which can become
// stale in slow CI environments where CRD registration takes time to fully propagate.
//
//nolint:cyclop,funlen,gocognit // polling loop with retry logic requires multiple conditional branches
func ensureLocalOCIRepositoryInsecure(
	ctx context.Context,
	restConfig *rest.Config,
) error {
	waitCtx, cancel := context.WithTimeout(ctx, fluxAPIAvailabilityTimeout)
	defer cancel()

	ticker := time.NewTicker(fluxAPIAvailabilityPollInterval)
	defer ticker.Stop()

	// Define the GVR for OCIRepository - this is the key to bypassing REST mapper cache.
	// With dynamic client + GVR, we don't rely on discovery at all.
	ociRepoGVR := schema.GroupVersionResource{
		Group:    sourcev1.GroupVersion.Group,
		Version:  sourcev1.GroupVersion.Version,
		Resource: "ocirepositories",
	}

	var lastErr error

	for {
		// Create a fresh dynamic client on each retry.
		// Unlike the typed client, this doesn't require REST mapper discovery.
		dynamicClient, clientErr := dynamic.NewForConfig(restConfig)
		if clientErr != nil {
			lastErr = clientErr

			select {
			case <-waitCtx.Done():
				return fmt.Errorf(
					"timed out creating dynamic client for OCIRepository: %w",
					lastErr,
				)
			case <-ticker.C:
				continue
			}
		}

		// Get the OCIRepository using the dynamic client
		unstructuredRepo, err := dynamicClient.Resource(ociRepoGVR).
			Namespace(fluxclient.DefaultNamespace).
			Get(waitCtx, defaultOCIRepositoryName, metav1.GetOptions{})

		switch {
		case err == nil:
			// Check if already insecure
			insecure, found, _ := unstructured.NestedBool(
				unstructuredRepo.Object,
				"spec",
				"insecure",
			)
			if found && insecure {
				return nil
			}

			// Patch to set insecure: true
			err = unstructured.SetNestedField(unstructuredRepo.Object, true, "spec", "insecure")
			if err != nil {
				return fmt.Errorf("failed to set insecure field: %w", err)
			}

			_, updateErr := dynamicClient.Resource(ociRepoGVR).
				Namespace(fluxclient.DefaultNamespace).
				Update(ctx, unstructuredRepo, metav1.UpdateOptions{})
			if updateErr == nil {
				return nil
			}

			// If the update fails with a transient API error, retry
			if isTransientAPIError(updateErr) {
				lastErr = updateErr

				select {
				case <-waitCtx.Done():
					return fmt.Errorf(
						"timed out updating OCIRepository %s/%s: %w",
						fluxclient.DefaultNamespace,
						defaultOCIRepositoryName,
						lastErr,
					)
				case <-ticker.C:
					continue
				}
			}

			return fmt.Errorf(
				"failed to update OCIRepository %s/%s: %w",
				fluxclient.DefaultNamespace,
				defaultOCIRepositoryName,
				updateErr,
			)
		case apierrors.IsNotFound(err):
			select {
			case <-waitCtx.Done():
				return errOCIRepositoryCreateTimout
			case <-ticker.C:
			}
		default:
			lastErr = err

			// Handle "no matches for kind" errors and other API errors by retrying
			select {
			case <-waitCtx.Done():
				return fmt.Errorf(
					"timed out waiting for OCIRepository CRD to be ready: %w",
					lastErr,
				)
			case <-ticker.C:
				// Continue waiting - CRD might not be fully registered yet
			}
		}
	}
}

func normalizeFluxPath() string {
	// Flux expects paths to be relative to the root of the unpacked artifact.
	return "./"
}

// pollUntilReady implements a generic polling pattern for waiting on async conditions.
// It repeatedly calls checkFn until it returns true (success) or the context expires.
// Returns the last error encountered if the wait times out.
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

			return fmt.Errorf("timed out waiting for %s: %w", resourceDesc, lastErr)
		case <-ticker.C:
		}
	}
}

func waitForGroupVersion(
	ctx context.Context,
	restConfig *rest.Config,
	groupVersion schema.GroupVersion,
) error {
	return pollUntilReady(
		ctx,
		fluxAPIAvailabilityTimeout,
		fluxAPIAvailabilityPollInterval,
		"API "+groupVersion.String(),
		func() (bool, error) {
			discoveryClient, err := newDiscoveryClient(restConfig)
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
func waitForCRDEstablished(
	ctx context.Context,
	restConfig *rest.Config,
	crdName string,
) error {
	waitCtx, cancel := context.WithTimeout(ctx, fluxAPIAvailabilityTimeout)
	defer cancel()

	ticker := time.NewTicker(fluxAPIAvailabilityPollInterval)
	defer ticker.Stop()

	apiextClient, err := newAPIExtensionsClient(restConfig)
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
// updates to include the new resource type. During this window, requests to the
// resource API will fail with "the server could not find the requested resource".
func waitForResourceAPIServable(
	ctx context.Context,
	restConfig *rest.Config,
	groupVersion schema.GroupVersion,
) error {
	return pollUntilReady(
		ctx,
		fluxAPIAvailabilityTimeout,
		fluxAPIAvailabilityPollInterval,
		fmt.Sprintf("API %s to be servable", groupVersion.String()),
		func() (bool, error) {
			resources, err := tryDiscoverResources(restConfig, groupVersion)
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
func tryDiscoverResources(
	restConfig *rest.Config,
	groupVersion schema.GroupVersion,
) (*metav1.APIResourceList, error) {
	// Create a new discovery client on each call to avoid caching issues.
	// The discovery client caches API group information, and a stale cache
	// might not reflect newly-registered resources.
	discoveryClient, err := newDiscoveryClient(restConfig)
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
