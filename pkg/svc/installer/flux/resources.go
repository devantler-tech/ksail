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
	registry "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/registry"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
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
)

var errKubeconfigRequired = errors.New("kubeconfig path is required")

var errCRDNotEstablished = errors.New("CRD is not yet established")

//nolint:gochecknoglobals // package-level timeout constants
var (
	fluxAPIAvailabilityTimeout      = 2 * time.Minute
	fluxAPIAvailabilityPollInterval = 2 * time.Second
)

var (
	errInvalidClusterConfig = errors.New("cluster configuration is required")

	//nolint:gochecknoglobals // Allows mocking REST config for tests
	loadRESTConfig = buildRESTConfig

	//nolint:noinlineerr,gochecknoglobals // error handling in scheme registration, allows mocking for tests
	newFluxResourcesClient = func(restConfig *rest.Config) (client.Client, error) {
		scheme := runtime.NewScheme()

		if err := addFluxInstanceToScheme(scheme); err != nil {
			return nil, fmt.Errorf("failed to add flux instance scheme: %w", err)
		}

		if err := sourcev1.AddToScheme(scheme); err != nil {
			return nil, fmt.Errorf("failed to add flux source scheme: %w", err)
		}

		fluxClient, err := client.New(restConfig, client.Options{Scheme: scheme})
		if err != nil {
			return nil, fmt.Errorf("failed to create flux resource client: %w", err)
		}

		return fluxClient, nil
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

		apiextClient, err := client.New(restConfig, client.Options{Scheme: scheme})
		if err != nil {
			return nil, fmt.Errorf("failed to create apiextensions client: %w", err)
		}

		return apiextClient, nil
	}
)

// EnsureDefaultResources configures a default FluxInstance so the operator can
// bootstrap controllers and sync from the local OCI registry.
//
//nolint:contextcheck // context passed from caller and used in nested functions
func EnsureDefaultResources(
	ctx context.Context,
	kubeconfig string,
	clusterCfg *v1alpha1.Cluster,
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

	// Wait for FluxInstance API to be fully ready
	err = waitForAPIReady(ctx, restConfig, fluxInstanceGroupVersion, fluxInstanceCRDName)
	if err != nil {
		return err
	}

	fluxInstance, err := buildFluxInstance(clusterCfg)
	if err != nil {
		return err
	}

	fluxClient, err := newFluxResourcesClient(restConfig)
	if err != nil {
		return err
	}

	err = upsertFluxResource(ctx, fluxClient, fluxInstance)
	if err != nil {
		return err
	}

	// Wait for OCIRepository API to be fully ready
	err = waitForAPIReady(ctx, restConfig, sourcev1.GroupVersion, ociRepositoriesCRDName)
	if err != nil {
		return err
	}

	if clusterCfg.Spec.Cluster.LocalRegistry == v1alpha1.LocalRegistryEnabled {
		return ensureLocalOCIRepositoryInsecure(ctx, fluxClient)
	}

	return nil
}

// waitForAPIReady waits for both the API to be discoverable and the CRD to be established.
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
	return waitForCRDEstablished(ctx, restConfig, crdName)
}

//nolint:unparam // error return kept for consistency with resource building patterns
func buildFluxInstance(clusterCfg *v1alpha1.Cluster) (*FluxInstance, error) {
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
	repoHost := registry.LocalRegistryClusterHost
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

func upsertFluxResource(
	ctx context.Context,
	fluxClient client.Client,
	obj client.Object,
) error {
	key := client.ObjectKeyFromObject(obj)

	switch desired := obj.(type) {
	case *FluxInstance:
		return upsertFluxInstanceWithRetry(ctx, fluxClient, key, desired)
	default:
		//nolint:err113 // type information is dynamic and necessary for debugging
		return fmt.Errorf("unsupported Flux resource type %T", obj)
	}
}

// upsertFluxInstanceWithRetry creates or updates a FluxInstance with retry logic
// to handle transient API errors during CRD initialization.
//

func upsertFluxInstanceWithRetry(
	ctx context.Context,
	fluxClient client.Client,
	key client.ObjectKey,
	desired *FluxInstance,
) error {
	waitCtx, cancel := context.WithTimeout(ctx, fluxAPIAvailabilityTimeout)
	defer cancel()

	ticker := time.NewTicker(fluxAPIAvailabilityPollInterval)
	defer ticker.Stop()

	var lastErr error

	for {
		err := tryUpsertFluxInstance(waitCtx, fluxClient, key, desired)
		if err == nil {
			return nil
		}

		// If the error is a transient API error (like "resource not found" during CRD init),
		// retry. Otherwise, return the error immediately.
		if !isTransientAPIError(err) {
			return err
		}

		lastErr = err

		select {
		case <-waitCtx.Done():
			if lastErr == nil {
				lastErr = waitCtx.Err()
			}

			return fmt.Errorf(
				"timed out upserting FluxInstance %s/%s: %w",
				key.Namespace,
				key.Name,
				lastErr,
			)
		case <-ticker.C:
			// Retry
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

	// "the server could not find the requested resource" is typically an IsNotFound
	// error at the API level (not the same as a resource not existing)
	errMsg := err.Error()

	return strings.Contains(errMsg, "the server could not find the requested resource") ||
		strings.Contains(errMsg, "no matches for kind")
}

//nolint:cyclop // polling loop requires multiple conditional branches for different error types
func ensureLocalOCIRepositoryInsecure(ctx context.Context, fluxClient client.Client) error {
	key := client.ObjectKey{Name: defaultOCIRepositoryName, Namespace: fluxclient.DefaultNamespace}

	waitCtx, cancel := context.WithTimeout(ctx, fluxAPIAvailabilityTimeout)
	defer cancel()

	ticker := time.NewTicker(fluxAPIAvailabilityPollInterval)
	defer ticker.Stop()

	for {
		repo := &sourcev1.OCIRepository{}

		err := fluxClient.Get(waitCtx, key, repo)
		switch {
		case err == nil:
			if repo.Spec.Insecure {
				return nil
			}

			repo.Spec.Insecure = true

			updateErr := fluxClient.Update(ctx, repo)
			if updateErr != nil {
				return fmt.Errorf(
					"failed to update OCIRepository %s/%s: %w",
					key.Namespace,
					key.Name,
					updateErr,
				)
			}

			return nil
		case apierrors.IsNotFound(err):
			select {
			//nolint:err113 // dynamic resource key necessary for debugging timeout
			case <-waitCtx.Done():
				return fmt.Errorf(
					"timed out waiting for OCIRepository %s/%s to be created by FluxInstance",
					key.Namespace,
					key.Name,
				)
			case <-ticker.C:
			}
		default:
			// Handle "no matches for kind" errors and other API errors by retrying
			select {
			case <-waitCtx.Done():
				return fmt.Errorf("timed out waiting for OCIRepository CRD to be ready: %w", err)
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

func waitForGroupVersion(
	ctx context.Context,
	restConfig *rest.Config,
	groupVersion schema.GroupVersion,
) error {
	waitCtx, cancel := context.WithTimeout(ctx, fluxAPIAvailabilityTimeout)
	defer cancel()

	ticker := time.NewTicker(fluxAPIAvailabilityPollInterval)
	defer ticker.Stop()

	var lastErr error

	for {
		// Create a new discovery client on each iteration to avoid caching issues
		discoveryClient, err := newDiscoveryClient(restConfig)
		if err != nil {
			return fmt.Errorf("failed to create discovery client: %w", err)
		}

		_, err = discoveryClient.ServerResourcesForGroupVersion(groupVersion.String())
		if err == nil {
			return nil
		}

		lastErr = err

		select {
		case <-waitCtx.Done():
			if lastErr == nil {
				lastErr = waitCtx.Err()
			}

			return fmt.Errorf("timed out waiting for API %s: %w", groupVersion.String(), lastErr)
		case <-ticker.C:
		}
	}
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

func buildRESTConfig(kubeconfig string) (*rest.Config, error) {
	if strings.TrimSpace(kubeconfig) == "" {
		return nil, errKubeconfigRequired
	}

	restConfig, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("failed to load kubeconfig %s: %w", kubeconfig, err)
	}

	return restConfig, nil
}
