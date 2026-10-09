package helm

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"

	fluxv2 "github.com/fluxcd/helm-controller/api/v2"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
)

var errFluxReleaseNotOwned = errors.New("no matching Flux HelmRelease")

const fluxOwnershipPageSize = 200

// fluxReleaseOwnershipLabels recognizes the declared owner independently of
// Helm's storage labels. A missing Flux API is normal; unreadable ownership is
// not proof that it is safe to mutate a release.
func (c *Client) fluxReleaseOwnershipLabels(
	ctx context.Context, releaseName, namespace string,
) (map[string]string, error) {
	config, err := c.settings.RESTClientGetter().ToRESTConfig()
	if err != nil {
		return nil, fmt.Errorf("get REST config for Flux release ownership: %w", err)
	}

	client, err := dynamic.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("create client for Flux release ownership: %w", err)
	}

	resource := client.Resource(schema.GroupVersionResource{
		Group: "helm.toolkit.fluxcd.io", Version: "v2", Resource: "helmreleases",
	})
	options := metav1.ListOptions{Limit: fluxOwnershipPageSize}

	for {
		list, listErr := resource.List(ctx, options)
		if apierrors.IsNotFound(listErr) && options.Continue == "" {
			return nil, errFluxReleaseNotOwned
		}

		if listErr != nil {
			return nil, fmt.Errorf("read Flux release ownership: %w", listErr)
		}

		labels, ownershipErr := fluxOwnershipLabels(list.Items, releaseName, namespace)
		if !errors.Is(ownershipErr, errFluxReleaseNotOwned) {
			return labels, ownershipErr
		}

		options.Continue = list.GetContinue()
		if options.Continue == "" {
			return nil, errFluxReleaseNotOwned
		}
	}
}

// fluxOwnershipLabels checks one page without treating malformed declarations
// as permission to change a release.
func fluxOwnershipLabels(
	items []unstructured.Unstructured, releaseName, namespace string,
) (map[string]string, error) {
	for _, item := range items {
		var owner fluxv2.HelmRelease

		err := runtime.DefaultUnstructuredConverter.FromUnstructured(item.Object, &owner)
		if err != nil {
			return nil, fmt.Errorf("decode Flux release ownership: %w", err)
		}

		if fluxReleaseMatches(owner, releaseName, namespace) {
			return map[string]string{
				"helm.toolkit.fluxcd.io/name":      owner.Name,
				"helm.toolkit.fluxcd.io/namespace": owner.Namespace,
			}, nil
		}
	}

	return nil, errFluxReleaseNotOwned
}

// fluxReleaseMatches binds ownership to this cluster and the target namespace,
// not the HelmRelease's namespace or its independently configured storage.
func fluxReleaseMatches(owner fluxv2.HelmRelease, name, namespace string) bool {
	if owner.Spec.KubeConfig != nil {
		return false
	}

	declared := owner.GetReleaseName()
	// Flux shortens names longer than Helm's 53-byte maximum to a 40-byte
	// prefix and 12 hex characters of SHA-256, separated by a hyphen.
	// https://github.com/fluxcd/helm-controller/blob/v1.5.5/internal/release/name.go
	const helmNameLimit, prefixLength, digestLength = 53, 40, 12

	if len(declared) > helmNameLimit {
		digest := fmt.Sprintf("%x", sha256.Sum256([]byte(declared)))
		declared = declared[:prefixLength] + "-" + digest[:digestLength]
	}

	if declared == name && owner.GetReleaseNamespace() == namespace {
		return true
	}

	// Also protect a still-owned release while Flux reconciles a name change.
	for _, snapshot := range owner.Status.History {
		if snapshot.Name == name && snapshot.Namespace == namespace {
			return true
		}
	}

	return false
}
