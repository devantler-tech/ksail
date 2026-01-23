package fluxinstaller

import (
	"context"
	"fmt"
	"time"

	fluxclient "github.com/devantler-tech/ksail/v5/pkg/client/flux"
	sourcev1 "github.com/fluxcd/source-controller/api/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
)

const (
	defaultOCIRepositoryName = fluxclient.DefaultNamespace
	ociRepositoriesCRDName   = "ocirepositories.source.toolkit.fluxcd.io"
)

// ociRepositoryPatcher handles patching OCIRepository resources for local registries.
type ociRepositoryPatcher struct {
	restConfig *rest.Config
	apiWaiter  *apiWaiter
}

// newOCIRepositoryPatcher creates a new OCIRepository patcher.
func newOCIRepositoryPatcher(
	restConfig *rest.Config,
	timeout, interval time.Duration,
) *ociRepositoryPatcher {
	return &ociRepositoryPatcher{
		restConfig: restConfig,
		apiWaiter:  newAPIWaiter(restConfig, timeout, interval),
	}
}

// waitForAPI waits for the OCIRepository API to be available with stabilization delay.
func (p *ociRepositoryPatcher) waitForAPI(ctx context.Context) error {
	err := p.apiWaiter.waitForAPIReady(ctx, sourcev1.GroupVersion, ociRepositoriesCRDName)
	if err != nil {
		return err
	}

	// Brief stabilization delay for OCIRepository API as well
	select {
	case <-ctx.Done():
		return fmt.Errorf("context cancelled during OCIRepository API stabilization: %w", ctx.Err())
	case <-time.After(apiStabilizationDelay):
	}

	return nil
}

// ensureInsecure patches the OCIRepository with insecure: true for local registries.
// Uses a dynamic Kubernetes client to bypass REST mapper cache issues.
func (p *ociRepositoryPatcher) ensureInsecure(ctx context.Context) error {
	waitCtx, cancel := context.WithTimeout(ctx, p.apiWaiter.timeout)
	defer cancel()

	ticker := time.NewTicker(p.apiWaiter.interval)
	defer ticker.Stop()

	// Define the GVR for OCIRepository - bypasses REST mapper cache.
	ociRepoGVR := schema.GroupVersionResource{
		Group:    sourcev1.GroupVersion.Group,
		Version:  sourcev1.GroupVersion.Version,
		Resource: "ocirepositories",
	}

	var lastErr error

	for {
		result, err := p.tryPatchInsecure(ctx, waitCtx, ociRepoGVR)

		switch result {
		case patchSuccess:
			return nil
		case patchRetry:
			lastErr = err
		case patchFailed:
			return err
		}

		select {
		case <-waitCtx.Done():
			if lastErr == nil {
				lastErr = waitCtx.Err()
			}

			return fmt.Errorf("timed out waiting for OCIRepository: %w", lastErr)
		case <-ticker.C:
			// Continue retry loop
		}
	}
}

type patchResult int

const (
	patchSuccess patchResult = iota
	patchRetry
	patchFailed
)

// tryPatchInsecure attempts to patch the OCIRepository once.
// Returns whether to retry, succeed, or fail permanently.
func (p *ociRepositoryPatcher) tryPatchInsecure(
	ctx context.Context,
	waitCtx context.Context,
	gvr schema.GroupVersionResource,
) (patchResult, error) {
	dynamicClient, clientErr := dynamic.NewForConfig(p.restConfig)
	if clientErr != nil {
		return patchRetry, fmt.Errorf("failed to create dynamic client: %w", clientErr)
	}

	// Get the OCIRepository
	unstructuredRepo, err := dynamicClient.Resource(gvr).
		Namespace(fluxclient.DefaultNamespace).
		Get(waitCtx, defaultOCIRepositoryName, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return patchRetry, errOCIRepositoryCreateTimout
		}

		return patchRetry, fmt.Errorf("failed to get OCIRepository: %w", err)
	}

	// Check if already insecure
	insecure, found, _ := unstructured.NestedBool(unstructuredRepo.Object, "spec", "insecure")
	if found && insecure {
		return patchSuccess, nil
	}

	// Patch to set insecure: true
	err = unstructured.SetNestedField(unstructuredRepo.Object, true, "spec", "insecure")
	if err != nil {
		return patchFailed, fmt.Errorf("failed to set insecure field: %w", err)
	}

	_, updateErr := dynamicClient.Resource(gvr).
		Namespace(fluxclient.DefaultNamespace).
		Update(ctx, unstructuredRepo, metav1.UpdateOptions{})
	if updateErr == nil {
		return patchSuccess, nil
	}

	if isTransientAPIError(updateErr) {
		return patchRetry, fmt.Errorf("transient error updating OCIRepository: %w", updateErr)
	}

	return patchFailed, fmt.Errorf(
		"failed to update OCIRepository %s/%s: %w",
		fluxclient.DefaultNamespace, defaultOCIRepositoryName, updateErr,
	)
}
