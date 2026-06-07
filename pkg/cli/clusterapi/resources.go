package clusterapi

import (
	"context"
	"fmt"

	"github.com/devantler-tech/ksail/v7/pkg/k8s"
	"github.com/devantler-tech/ksail/v7/pkg/operator/api"
	clusterdetector "github.com/devantler-tech/ksail/v7/pkg/svc/detector/cluster"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/clientcmd"
)

// Ensure the local backend exposes the read-only resource browser.
var _ api.ResourceService = (*Service)(nil)

// dynamicClientFunc builds a dynamic client for the named local cluster. Injectable so tests can
// substitute a fake client instead of resolving a real kubeconfig context.
type dynamicClientFunc func(ctx context.Context, clusterName string) (dynamic.Interface, error)

// defaultDynamicClient resolves the kubeconfig context for a local cluster by name and builds a
// dynamic client against it. The cluster name (as the UI lists it) is matched to a context using the
// same distribution-context patterns the detector uses (kind-<name>, k3d-<name>, admin@<name>, …).
func defaultDynamicClient(_ context.Context, clusterName string) (dynamic.Interface, error) {
	kubeconfigPath := k8s.DefaultKubeconfigPath()

	contextName, err := contextForCluster(kubeconfigPath, clusterName)
	if err != nil {
		return nil, err
	}

	client, err := k8s.NewDynamicClient(kubeconfigPath, contextName)
	if err != nil {
		return nil, fmt.Errorf("build dynamic client for %q: %w", clusterName, err)
	}

	return client, nil
}

// contextForCluster finds the kubeconfig context whose detected cluster name matches clusterName.
// Returns an ErrNotFound-wrapped error (→ 404) when no context matches.
func contextForCluster(kubeconfigPath, clusterName string) (string, error) {
	config, err := clientcmd.LoadFromFile(kubeconfigPath)
	if err != nil {
		return "", fmt.Errorf("load kubeconfig %q: %w", kubeconfigPath, err)
	}

	for contextName := range config.Contexts {
		_, name, detectErr := clusterdetector.DetectDistributionFromContext(contextName)
		if detectErr == nil && name == clusterName {
			return contextName, nil
		}
	}

	return "", fmt.Errorf("%w: no kubeconfig context for cluster %q", api.ErrNotFound, clusterName)
}

// ListResources lists resources of the requested (allowlisted) kind from the named cluster. A
// namespaced kind with an empty query namespace lists across all namespaces.
func (s *Service) ListResources(
	ctx context.Context,
	_, name string,
	query api.ResourceQuery,
) (*unstructured.UnstructuredList, error) {
	kind, err := api.ResourceKindFor(query.Kind)
	if err != nil {
		return nil, fmt.Errorf("resolve resource kind: %w", err)
	}

	client, err := s.newDynamicClient(ctx, name)
	if err != nil {
		return nil, err
	}

	var lister dynamic.ResourceInterface = client.Resource(kind.GVR)
	if kind.Namespaced && query.Namespace != "" {
		lister = client.Resource(kind.GVR).Namespace(query.Namespace)
	}

	list, err := lister.List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", query.Kind, err)
	}

	return list, nil
}

// GetResource fetches a single resource of the requested (allowlisted) kind from the named cluster.
func (s *Service) GetResource(
	ctx context.Context,
	_, name string,
	ref api.ResourceRef,
) (*unstructured.Unstructured, error) {
	kind, err := api.ResourceKindFor(ref.Kind)
	if err != nil {
		return nil, fmt.Errorf("resolve resource kind: %w", err)
	}

	client, err := s.newDynamicClient(ctx, name)
	if err != nil {
		return nil, err
	}

	var getter dynamic.ResourceInterface = client.Resource(kind.GVR)
	if kind.Namespaced {
		getter = client.Resource(kind.GVR).Namespace(ref.Namespace)
	}

	obj, err := getter.Get(ctx, ref.Name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("get %s %q: %w", ref.Kind, ref.Name, err)
	}

	return obj, nil
}
