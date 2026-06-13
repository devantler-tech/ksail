package api

import (
	"context"
	"fmt"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
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
// managed (child) cluster, implementing ResourceService and ResourceWriter (the workload browser and
// its safe write actions). The resolve-then-delegate boilerplate lives in the embedded ResourceAdapter;
// this type supplies only ResourceClient (the vcluster connection). It is returned only by
// NewCRClusterServiceWithResources, so a plain operator service (NewCRClusterService) without a
// child-cluster resolver does not advertise the resource capability.
type crConnectedClusterService struct {
	*crClusterService
	ResourceAdapter

	newDynamicClient childDynamicClientFunc
}

// Ensure the connected operator backend exposes the read-only resource browser and the safe write
// actions (scale, rollout restart, delete, GitOps reconcile) via the shared adapter.
var (
	_ ResourceService        = (*crConnectedClusterService)(nil)
	_ ResourceWriter         = (*crConnectedClusterService)(nil)
	_ ResourceClientProvider = (*crConnectedClusterService)(nil)
	// Inherited from the embedded *crClusterService: in-place update and component-install advertising
	// (componentsInstall=true) carry through to the connected operator backend.
	_ ClusterUpdater     = (*crConnectedClusterService)(nil)
	_ ComponentInstaller = (*crConnectedClusterService)(nil)
)

// NewCRClusterServiceWithResources returns an operator ClusterService that can also browse resources in
// each cluster's managed child cluster, using newDynamicClient to connect to it.
func NewCRClusterServiceWithResources(
	kubeClient client.Client,
	newDynamicClient childDynamicClientFunc,
) ClusterService {
	service := &crConnectedClusterService{
		crClusterService: &crClusterService{client: kubeClient},
		newDynamicClient: newDynamicClient,
	}
	service.ResourceAdapter = ResourceAdapter{Provider: service}

	return service
}

// ResourceClient resolves the named Cluster CR and builds a dynamic client against its managed child
// cluster — the one per-backend difference behind the shared workload-browser surface.
func (s *crConnectedClusterService) ResourceClient(
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
