package clusterapi

import (
	"context"
	"fmt"

	"github.com/devantler-tech/ksail/v7/pkg/k8s"
	"github.com/devantler-tech/ksail/v7/pkg/operator/api"
	clusterdetector "github.com/devantler-tech/ksail/v7/pkg/svc/detector/cluster"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// Ensure the local backend exposes the read-only resource browser and the safe write actions.
var (
	_ api.ResourceService = (*Service)(nil)
	_ api.ResourceWriter  = (*Service)(nil)
)

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

// restConfigForCluster resolves the cluster's kubeconfig context by name and builds a *rest.Config —
// the shared preamble for the apply (dynamic + RESTMapper) and exec (clientset) client builders.
func restConfigForCluster(clusterName string) (*rest.Config, error) {
	kubeconfigPath := k8s.DefaultKubeconfigPath()

	contextName, err := contextForCluster(kubeconfigPath, clusterName)
	if err != nil {
		return nil, err
	}

	restConfig, err := k8s.BuildRESTConfig(kubeconfigPath, contextName)
	if err != nil {
		return nil, fmt.Errorf("build rest config for %q: %w", clusterName, err)
	}

	return restConfig, nil
}

// resolveKindAndClient resolves an allowlisted kind to its GVR mapping and builds a dynamic client
// for the named cluster — the shared preamble of every resource read/write method.
func (s *Service) resolveKindAndClient(
	ctx context.Context,
	clusterName, kindName string,
) (api.ResourceKind, dynamic.Interface, error) {
	kind, err := api.ResourceKindFor(kindName)
	if err != nil {
		return api.ResourceKind{}, nil, fmt.Errorf("resolve resource kind: %w", err)
	}

	client, err := s.newDynamicClient(ctx, clusterName)
	if err != nil {
		return api.ResourceKind{}, nil, err
	}

	return kind, client, nil
}

// ListResources lists resources of the requested (allowlisted) kind from the named cluster. A
// namespaced kind with an empty query namespace lists across all namespaces.
func (s *Service) ListResources(
	ctx context.Context,
	_, name string,
	query api.ResourceQuery,
) (*unstructured.UnstructuredList, error) {
	kind, client, err := s.resolveKindAndClient(ctx, name, query.Kind)
	if err != nil {
		return nil, err
	}

	list, err := api.ListResourcesWith(ctx, client, kind, query)
	if err != nil {
		return nil, fmt.Errorf("read resources from cluster %q: %w", name, err)
	}

	return list, nil
}

// GetResource fetches a single resource of the requested (allowlisted) kind from the named cluster.
func (s *Service) GetResource(
	ctx context.Context,
	_, name string,
	ref api.ResourceRef,
) (*unstructured.Unstructured, error) {
	kind, client, err := s.resolveKindAndClient(ctx, name, ref.Kind)
	if err != nil {
		return nil, err
	}

	obj, err := api.GetResourceWith(ctx, client, kind, ref)
	if err != nil {
		return nil, fmt.Errorf("read resource from cluster %q: %w", name, err)
	}

	return obj, nil
}

// ScaleResource sets the replica count of a scalable workload. The validation and merge-patch logic
// is shared with the operator backend via api.ScaleResourceWith; only the dynamic-client resolution
// (kubeconfig context for the named local cluster) differs.
func (s *Service) ScaleResource(
	ctx context.Context,
	_, name string,
	ref api.ResourceRef,
	replicas int32,
) error {
	client, err := s.newDynamicClient(ctx, name)
	if err != nil {
		return err
	}

	err = api.ScaleResourceWith(ctx, client, ref, replicas)
	if err != nil {
		return fmt.Errorf("scale resource in cluster %q: %w", name, err)
	}

	return nil
}

// RestartResource triggers a rolling restart of a workload, delegating the restartedAt-annotation
// patch to the shared api.RestartResourceWith.
func (s *Service) RestartResource(ctx context.Context, _, name string, ref api.ResourceRef) error {
	client, err := s.newDynamicClient(ctx, name)
	if err != nil {
		return err
	}

	err = api.RestartResourceWith(ctx, client, ref)
	if err != nil {
		return fmt.Errorf("restart resource in cluster %q: %w", name, err)
	}

	return nil
}

// ReconcileResource triggers an immediate GitOps reconcile of a Flux/ArgoCD resource, delegating the
// engine-specific annotation patch to the shared api.ReconcileResourceWith.
func (s *Service) ReconcileResource(
	ctx context.Context,
	_, name string,
	ref api.ResourceRef,
) error {
	client, err := s.newDynamicClient(ctx, name)
	if err != nil {
		return err
	}

	err = api.ReconcileResourceWith(ctx, client, ref)
	if err != nil {
		return fmt.Errorf("reconcile resource in cluster %q: %w", name, err)
	}

	return nil
}

// DeleteResource deletes a namespaced allowlisted resource, delegating the namespaced-only guard and
// delete to the shared api.DeleteResourceWith.
func (s *Service) DeleteResource(ctx context.Context, _, name string, ref api.ResourceRef) error {
	client, err := s.newDynamicClient(ctx, name)
	if err != nil {
		return err
	}

	err = api.DeleteResourceWith(ctx, client, ref)
	if err != nil {
		return fmt.Errorf("delete resource in cluster %q: %w", name, err)
	}

	return nil
}
