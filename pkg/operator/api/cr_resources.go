package api

import (
	"context"
	"fmt"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// childDynamicClientFunc builds a dynamic client for a Cluster's managed (child) cluster. The
// connection logic lives in pkg/operator, which already imports this package, so it is injected here
// rather than imported — avoiding an import cycle.
type childDynamicClientFunc func(
	ctx context.Context,
	cluster *v1alpha1.Cluster,
) (dynamic.Interface, error)

// crConnectedClusterService extends the operator's CRUD backend with read access to each cluster's
// managed (child) cluster, implementing ResourceService (the workload browser). It is returned only by
// NewCRClusterServiceWithResources, so a plain operator service (NewCRClusterService) without a
// child-cluster resolver does not advertise the resource capability.
type crConnectedClusterService struct {
	*crClusterService

	newDynamicClient childDynamicClientFunc
}

// Ensure the connected operator backend exposes the read-only resource browser.
var _ ResourceService = (*crConnectedClusterService)(nil)

// NewCRClusterServiceWithResources returns an operator ClusterService that can also browse resources in
// each cluster's managed child cluster, using newDynamicClient to connect to it.
func NewCRClusterServiceWithResources(
	kubeClient client.Client,
	newDynamicClient childDynamicClientFunc,
) ClusterService {
	return &crConnectedClusterService{
		crClusterService: &crClusterService{client: kubeClient},
		newDynamicClient: newDynamicClient,
	}
}

// ListResources lists an allowlisted kind from the named cluster's managed child cluster.
func (s *crConnectedClusterService) ListResources(
	ctx context.Context,
	namespace, name string,
	query ResourceQuery,
) (*unstructured.UnstructuredList, error) {
	kind, err := ResourceKindFor(query.Kind)
	if err != nil {
		return nil, err
	}

	dyn, err := s.dynamicClientForCluster(ctx, namespace, name)
	if err != nil {
		return nil, err
	}

	return ListResourcesWith(ctx, dyn, kind, query)
}

// GetResource fetches one allowlisted resource from the named cluster's managed child cluster.
func (s *crConnectedClusterService) GetResource(
	ctx context.Context,
	namespace, name string,
	ref ResourceRef,
) (*unstructured.Unstructured, error) {
	kind, err := ResourceKindFor(ref.Kind)
	if err != nil {
		return nil, err
	}

	dyn, err := s.dynamicClientForCluster(ctx, namespace, name)
	if err != nil {
		return nil, err
	}

	return GetResourceWith(ctx, dyn, kind, ref)
}

// dynamicClientForCluster resolves the named Cluster CR and builds a dynamic client against its managed
// child cluster.
func (s *crConnectedClusterService) dynamicClientForCluster(
	ctx context.Context,
	namespace, name string,
) (dynamic.Interface, error) {
	cluster, err := s.Get(ctx, namespace, name)
	if err != nil {
		return nil, err
	}

	dyn, err := s.newDynamicClient(ctx, cluster)
	if err != nil {
		return nil, fmt.Errorf("connect to cluster %q: %w", name, err)
	}

	return dyn, nil
}
