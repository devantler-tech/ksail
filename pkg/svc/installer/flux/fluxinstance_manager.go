package fluxinstaller

import (
	"context"
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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	defaultSourceDirectory   = "k8s"
	defaultArtifactTag       = "dev"
	fluxIntervalFallback     = time.Minute
	fluxDistributionVersion  = "2.x"
	fluxDistributionRegistry = "ghcr.io/fluxcd"
	fluxDistributionArtifact = "oci://ghcr.io/controlplaneio-fluxcd/flux-operator-manifests:latest"
)

// instanceManager handles Instance lifecycle operations.
type instanceManager struct {
	restConfig *rest.Config
	apiWaiter  *apiWaiter
}

// newFluxInstanceManager creates a new Instance manager.
func newFluxInstanceManager(
	restConfig *rest.Config,
	timeout, interval time.Duration,
) *instanceManager {
	return &instanceManager{
		restConfig: restConfig,
		apiWaiter:  newAPIWaiter(restConfig, timeout, interval),
	}
}

// setup waits for the Instance CRD, creates the client, and upserts the Instance.
func (m *instanceManager) setup(
	ctx context.Context,
	clusterCfg *v1alpha1.Cluster,
	clusterName string,
) error {
	// Wait for Instance API to be fully ready
	err := m.apiWaiter.waitForAPIReady(ctx, fluxInstanceGroupVersion, fluxInstanceCRDName)
	if err != nil {
		return err
	}

	// Brief stabilization delay to allow the API server to fully propagate the CRD
	// across all its endpoints. This addresses race conditions observed in slower
	// CI environments (e.g., Talos on GitHub Actions) where discovery reports the
	// API as ready slightly before Create operations can succeed.
	select {
	case <-ctx.Done():
		return fmt.Errorf(
			"context cancelled during Instance API stabilization: %w",
			ctx.Err(),
		)
	case <-time.After(apiStabilizationDelay):
	}

	fluxInstance, err := buildInstance(clusterCfg, clusterName)
	if err != nil {
		return err
	}

	// Create a client factory that creates a fresh client on each retry.
	// This is necessary because the dynamic REST mapper caches discovery results,
	// and if the initial discovery happens before the API server fully propagates
	// the CRD, subsequent requests will fail until the cache expires.
	clientFactory := func() (client.Client, error) {
		return newFluxResourcesClient(m.restConfig)
	}

	return m.upsertWithRetry(ctx, clientFactory, fluxInstance)
}

// waitForReady waits for the Instance to report a Ready condition.
// The Instance controller sets this condition when Flux controllers are installed
// and the sync source (OCIRepository) is ready.
func (m *instanceManager) waitForReady(ctx context.Context) error {
	return pollUntilReady(
		ctx,
		m.apiWaiter.timeout,
		m.apiWaiter.interval,
		"Instance to be ready",
		func() (bool, error) {
			// Create a fresh client on each retry to avoid caching issues
			fluxClient, err := newFluxResourcesClient(m.restConfig)
			if err != nil {
				// Client creation errors are transient during CRD initialization.
				// Don't return error, just keep polling.
				//nolint:nilerr // Transient error - continue polling
				return false, nil
			}

			instance := &Instance{}
			key := client.ObjectKey{
				Name:      fluxInstanceDefaultName,
				Namespace: fluxclient.DefaultNamespace,
			}

			err = fluxClient.Get(ctx, key, instance)
			if err != nil {
				// All Get errors are transient (NotFound, API not ready, etc)
				// Don't return error, just keep polling.
				//nolint:nilerr // Transient error - continue polling
				return false, nil
			}

			// Check for Ready condition
			for _, condition := range instance.Status.Conditions {
				if condition.Type == "Ready" {
					if condition.Status == metav1.ConditionTrue {
						return true, nil
					}
					// Ready=False or Unknown - continue polling
				}
			}

			// Not ready yet, keep waiting
			return false, nil
		},
	)
}

// upsertWithRetry creates or updates a Instance with retry logic
// to handle transient API errors during CRD initialization.
func (m *instanceManager) upsertWithRetry(
	ctx context.Context,
	clientFactory func() (client.Client, error),
	desired *Instance,
) error {
	waitCtx, cancel := context.WithTimeout(ctx, m.apiWaiter.timeout)
	defer cancel()

	ticker := time.NewTicker(m.apiWaiter.interval)
	defer ticker.Stop()

	key := client.ObjectKeyFromObject(desired)

	var lastErr error

	for {
		fluxClient, clientErr := clientFactory()
		if clientErr != nil {
			lastErr = clientErr

			select {
			case <-waitCtx.Done():
				return fmt.Errorf(
					"timed out creating client for Instance %s/%s: %w",
					key.Namespace, key.Name, lastErr,
				)
			case <-ticker.C:
				continue
			}
		}

		err := m.tryUpsert(waitCtx, fluxClient, key, desired)
		if err == nil {
			return nil
		}

		if !isTransientAPIError(err) {
			return err
		}

		lastErr = err

		select {
		case <-waitCtx.Done():
			return fmt.Errorf(
				"timed out upserting Instance %s/%s: %w",
				key.Namespace, key.Name, lastErr,
			)
		case <-ticker.C:
			// Retry with a fresh client
		}
	}
}

// tryUpsert attempts to create or update a Instance once.
func (m *instanceManager) tryUpsert(
	ctx context.Context,
	fluxClient client.Client,
	key client.ObjectKey,
	desired *Instance,
) error {
	existing := &Instance{}

	err := fluxClient.Get(ctx, key, existing)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return m.createAndVerify(ctx, fluxClient, key, desired)
		}

		return fmt.Errorf("failed to get Instance %s/%s: %w", key.Namespace, key.Name, err)
	}

	existing.Spec = desired.Spec

	err = fluxClient.Update(ctx, existing)
	if err != nil {
		return fmt.Errorf("failed to update Instance %s/%s: %w", key.Namespace, key.Name, err)
	}

	return nil
}

// createAndVerify creates a Instance and verifies it was persisted.
func (m *instanceManager) createAndVerify(
	ctx context.Context,
	fluxClient client.Client,
	key client.ObjectKey,
	desired *Instance,
) error {
	createErr := fluxClient.Create(ctx, desired)
	if createErr != nil {
		return fmt.Errorf("create Instance %s/%s: %w", key.Namespace, key.Name, createErr)
	}

	// Verify the Instance was actually created by reading it back.
	existing := &Instance{}

	verifyErr := fluxClient.Get(ctx, key, existing)
	if verifyErr != nil {
		return fmt.Errorf(
			"Instance %s/%s was not persisted after create (verification failed): %w",
			key.Namespace, key.Name, verifyErr,
		)
	}

	return nil
}

// buildInstance constructs a Instance resource from cluster configuration.
func buildInstance(clusterCfg *v1alpha1.Cluster, clusterName string) (*Instance, error) {
	localRegistry := clusterCfg.Spec.Cluster.LocalRegistry

	var repoURL, pullSecret, tag string

	if localRegistry.IsExternal() {
		repoURL, pullSecret, tag = buildExternalRegistryURL(localRegistry)
	} else {
		repoURL = buildLocalRegistryURL(localRegistry, clusterCfg, clusterName)
	}

	// Use configured tag if provided, otherwise default
	if tag == "" {
		tag = defaultArtifactTag
	}

	intervalPtr := &metav1.Duration{Duration: fluxIntervalFallback}

	return &Instance{
		TypeMeta: metav1.TypeMeta{
			APIVersion: fluxInstanceGroupVersion.String(),
			Kind:       fluxInstanceKind,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      fluxInstanceDefaultName,
			Namespace: fluxclient.DefaultNamespace,
		},
		Spec: InstanceSpec{
			Distribution: Distribution{
				Version:  fluxDistributionVersion,
				Registry: fluxDistributionRegistry,
				Artifact: fluxDistributionArtifact,
			},
			Sync: &Sync{
				Kind:       fluxOCIRepositoryKind,
				URL:        repoURL,
				Ref:        tag,
				Path:       normalizeFluxPath(),
				Provider:   "generic",
				Interval:   intervalPtr,
				PullSecret: pullSecret,
			},
		},
	}, nil
}

// buildExternalRegistryURL builds the OCI URL for an external registry.
// Returns the URL, pull secret name, and optional tag override.
func buildExternalRegistryURL(localRegistry v1alpha1.LocalRegistry) (string, string, string) {
	parsed := localRegistry.Parse()
	// For external registries, build URL without port (HTTPS 443 is implicit)
	repoURL := fmt.Sprintf("oci://%s/%s", parsed.Host, parsed.Path)

	var pullSecret string
	if localRegistry.HasCredentials() {
		pullSecret = ExternalRegistrySecretName
	}

	return repoURL, pullSecret, parsed.Tag
}

// buildLocalRegistryURL builds the OCI URL for a local registry.
func buildLocalRegistryURL(
	localRegistry v1alpha1.LocalRegistry,
	clusterCfg *v1alpha1.Cluster,
	clusterName string,
) string {
	sourceDir := strings.TrimSpace(clusterCfg.Spec.Workload.SourceDirectory)
	if sourceDir == "" {
		sourceDir = defaultSourceDirectory
	}

	projectName := registry.SanitizeRepoName(sourceDir)
	repoHost := registry.BuildLocalRegistryName(clusterName)
	repoPort := dockerclient.DefaultRegistryPort

	if !localRegistry.Enabled() {
		hostPort := localRegistry.ResolvedPort()
		repoHost = registry.DefaultEndpointHost
		repoPort = int(hostPort)
	}

	return fmt.Sprintf(
		"oci://%s/%s",
		net.JoinHostPort(repoHost, strconv.Itoa(repoPort)),
		projectName,
	)
}

func normalizeFluxPath() string {
	// Flux expects paths to be relative to the root of the unpacked artifact.
	return "./"
}

// newFluxResourcesClient creates a client for Instance and OCIRepository resources.
//
//nolint:gochecknoglobals // Allows mocking for tests
var newFluxResourcesClient = func(restConfig *rest.Config) (client.Client, error) {
	scheme := runtime.NewScheme()

	err := addFluxInstanceToScheme(scheme)
	if err != nil {
		return nil, fmt.Errorf("failed to add flux instance scheme: %w", err)
	}

	err = sourcev1.AddToScheme(scheme)
	if err != nil {
		return nil, fmt.Errorf("failed to add flux source scheme: %w", err)
	}

	return newDynamicClient(restConfig, scheme)
}
