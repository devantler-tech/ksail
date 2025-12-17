package fluxinstaller

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/devantler-tech/ksail-go/pkg/apis/cluster/v1alpha1"
	fluxclient "github.com/devantler-tech/ksail-go/pkg/client/flux"
	registry "github.com/devantler-tech/ksail-go/pkg/svc/provisioner/registry"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	defaultProjectName       = "ksail-workloads"
	defaultSourceDirectory   = "k8s"
	defaultArtifactTag       = "latest"
	defaultOCIRepositoryName = fluxclient.DefaultNamespace
	fluxIntervalFallback     = time.Minute
	fluxDistributionVersion  = "2.x"
	fluxDistributionRegistry = "ghcr.io/fluxcd"
	fluxDistributionArtifact = "oci://ghcr.io/controlplaneio-fluxcd/flux-operator-manifests:latest"
)

var errKubeconfigRequired = errors.New("kubeconfig path is required")

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

	err = waitForGroupVersion(ctx, restConfig, fluxInstanceGroupVersion)
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

	err = waitForGroupVersion(ctx, restConfig, sourcev1.GroupVersion)
	if err != nil {
		return err
	}

	if clusterCfg.Spec.LocalRegistry == v1alpha1.LocalRegistryEnabled {
		return ensureLocalOCIRepositoryInsecure(ctx, fluxClient)
	}

	return nil
}

//nolint:unparam // error return kept for consistency with resource building patterns
func buildFluxInstance(clusterCfg *v1alpha1.Cluster) (*FluxInstance, error) {
	interval := clusterCfg.Spec.Options.Flux.Interval.Duration
	if interval <= 0 {
		interval = fluxIntervalFallback
	}

	hostPort := clusterCfg.Spec.Options.LocalRegistry.HostPort
	if hostPort == 0 {
		hostPort = v1alpha1.DefaultLocalRegistryPort
	}

	sourceDir := strings.TrimSpace(clusterCfg.Spec.SourceDirectory)
	if sourceDir == "" {
		sourceDir = defaultSourceDirectory
	}

	projectName := sanitizeFluxName(sourceDir, defaultProjectName)
	repoHost := registry.LocalRegistryClusterHost
	repoPort := registry.DefaultRegistryPort

	if clusterCfg.Spec.LocalRegistry != v1alpha1.LocalRegistryEnabled {
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
		existing := &FluxInstance{}

		err := fluxClient.Get(ctx, key, existing)
		if err != nil {
			if apierrors.IsNotFound(err) {
				createErr := fluxClient.Create(ctx, desired)
				if createErr != nil {
					return fmt.Errorf("create FluxInstance %s/%s: %w", key.Namespace, key.Name, createErr)
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
	default:
		//nolint:err113 // type information is dynamic and necessary for debugging
		return fmt.Errorf("unsupported Flux resource type %T", obj)
	}
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

//nolint:cyclop // name sanitization requires character-by-character validation
func sanitizeFluxName(value, fallback string) string {
	trimmed := strings.ToLower(strings.TrimSpace(value))
	if trimmed == "" {
		trimmed = fallback
	}

	var builder strings.Builder

	previousHyphen := false

	for _, char := range trimmed {
		switch {
		case char >= 'a' && char <= 'z':
			builder.WriteRune(char)

			previousHyphen = false
		case char >= '0' && char <= '9':
			builder.WriteRune(char)

			previousHyphen = false
		default:
			if !previousHyphen {
				builder.WriteRune('-')

				previousHyphen = true
			}
		}
	}

	sanitized := strings.Trim(builder.String(), "-")
	if sanitized == "" {
		sanitized = fallback
	}

	if len(sanitized) > validation.DNS1123LabelMaxLength {
		sanitized = sanitized[:validation.DNS1123LabelMaxLength]
		sanitized = strings.Trim(sanitized, "-")
	}

	if sanitized == "" {
		sanitized = fallback
	}

	if len(validation.IsDNS1123Label(sanitized)) == 0 {
		return sanitized
	}

	return fallback
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
