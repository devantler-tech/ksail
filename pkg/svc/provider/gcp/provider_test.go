package gcp_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"cloud.google.com/go/container/apiv1/containerpb"
	"github.com/devantler-tech/ksail/v7/pkg/client/gke"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/gcp"
	"github.com/googleapis/gax-go/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	grpcstatus "google.golang.org/grpc/status"
)

// errBoom is the sentinel the fakes fail with.
var errBoom = errors.New("boom")

// resizeCall records one SetNodePoolSize request the fake received.
type resizeCall struct {
	name  string
	count int32
}

// fakeClusterManager implements the GKE client's cluster-manager seam so the
// provider can be exercised without GCP credentials. Only the operations the
// provider uses are scripted; the rest fail the test if called.
type fakeClusterManager struct {
	t        *testing.T
	clusters []*containerpb.Cluster
	getErr   error
	listErr  error
	sizeErr  error
	resizes  []resizeCall
}

func (f *fakeClusterManager) CreateCluster(
	_ context.Context,
	_ *containerpb.CreateClusterRequest,
	_ ...gax.CallOption,
) (*containerpb.Operation, error) {
	f.t.Fatal("unexpected CreateCluster call")

	return nil, errBoom
}

func (f *fakeClusterManager) DeleteCluster(
	_ context.Context,
	_ *containerpb.DeleteClusterRequest,
	_ ...gax.CallOption,
) (*containerpb.Operation, error) {
	f.t.Fatal("unexpected DeleteCluster call")

	return nil, errBoom
}

func (f *fakeClusterManager) GetCluster(
	_ context.Context,
	req *containerpb.GetClusterRequest,
	_ ...gax.CallOption,
) (*containerpb.Cluster, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}

	for _, cluster := range f.clusters {
		if req.GetName() == clusterResourceName(cluster) {
			return cluster, nil
		}
	}

	return nil, fmt.Errorf("get cluster: %w", grpcstatus.Error(codes.NotFound, "cluster not found"))
}

func (f *fakeClusterManager) ListClusters(
	_ context.Context,
	_ *containerpb.ListClustersRequest,
	_ ...gax.CallOption,
) (*containerpb.ListClustersResponse, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}

	return &containerpb.ListClustersResponse{Clusters: f.clusters}, nil
}

func (f *fakeClusterManager) SetNodePoolSize(
	_ context.Context,
	req *containerpb.SetNodePoolSizeRequest,
	_ ...gax.CallOption,
) (*containerpb.Operation, error) {
	if f.sizeErr != nil {
		return nil, f.sizeErr
	}

	f.resizes = append(f.resizes, resizeCall{name: req.GetName(), count: req.GetNodeCount()})

	return &containerpb.Operation{Name: "op-resize", Status: containerpb.Operation_DONE}, nil
}

func (f *fakeClusterManager) GetOperation(
	_ context.Context,
	_ *containerpb.GetOperationRequest,
	_ ...gax.CallOption,
) (*containerpb.Operation, error) {
	f.t.Fatal("unexpected GetOperation call")

	return nil, errBoom
}

func (f *fakeClusterManager) Close() error {
	return nil
}

// clusterResourceName renders the fully-qualified name the provider is
// expected to request for a scripted cluster.
func clusterResourceName(cluster *containerpb.Cluster) string {
	return "projects/proj/locations/" + cluster.GetLocation() + "/clusters/" + cluster.GetName()
}

// newProvider builds a Provider over a fake-backed GKE client.
func newProvider(t *testing.T, fake *fakeClusterManager, location string) *gcp.Provider {
	t.Helper()

	fake.t = t

	client, err := gke.NewClient(
		t.Context(),
		gke.WithClusterManager(fake),
		gke.WithPollInterval(time.Millisecond),
	)
	require.NoError(t, err)

	prov, err := gcp.NewProvider(client, "proj", location)
	require.NoError(t, err)

	return prov
}

// demoCluster scripts one cluster with two node pools in europe-north1.
func demoCluster() *containerpb.Cluster {
	return &containerpb.Cluster{
		Name:     "demo",
		Location: "europe-north1",
		Endpoint: "34.88.0.1",
		NodePools: []*containerpb.NodePool{
			{
				Name:             "default-pool",
				InitialNodeCount: 3,
				Status:           containerpb.NodePool_RUNNING,
				Config:           &containerpb.NodeConfig{MachineType: "e2-standard-4"},
			},
			{
				Name:   "burst-pool",
				Status: containerpb.NodePool_PROVISIONING,
			},
		},
	}
}

func TestNewProviderRequiresClient(t *testing.T) {
	t.Parallel()

	_, err := gcp.NewProvider(nil, "proj", "")

	require.ErrorIs(t, err, gcp.ErrClientRequired)
}

func TestNewProviderRequiresProject(t *testing.T) {
	t.Parallel()

	client, err := gke.NewClient(t.Context(), gke.WithClusterManager(&fakeClusterManager{}))
	require.NoError(t, err)

	_, err = gcp.NewProvider(client, "", "")

	require.ErrorIs(t, err, gcp.ErrProjectRequired)
}

func TestListNodesCollapsesNodePools(t *testing.T) {
	t.Parallel()

	fake := &fakeClusterManager{clusters: []*containerpb.Cluster{demoCluster()}}
	prov := newProvider(t, fake, "europe-north1")

	nodes, err := prov.ListNodes(t.Context(), "demo")

	require.NoError(t, err)
	require.Len(t, nodes, 2)
	assert.Equal(t, provider.NodeInfo{
		Name:        "default-pool",
		ClusterName: "demo",
		Role:        "worker",
		State:       "RUNNING",
		ServerType:  "e2-standard-4",
	}, nodes[0])
	assert.Equal(t, "PROVISIONING", nodes[1].State)
}

func TestListNodesTranslatesNotFound(t *testing.T) {
	t.Parallel()

	fake := &fakeClusterManager{clusters: []*containerpb.Cluster{demoCluster()}}
	prov := newProvider(t, fake, "europe-north1")

	_, err := prov.ListNodes(t.Context(), "missing")

	require.ErrorIs(t, err, provider.ErrClusterNotFound)
}

func TestListNodesResolvesLocationWhenUnconfigured(t *testing.T) {
	t.Parallel()

	fake := &fakeClusterManager{clusters: []*containerpb.Cluster{demoCluster()}}
	prov := newProvider(t, fake, "")

	nodes, err := prov.ListNodes(t.Context(), "demo")

	require.NoError(t, err)
	assert.Len(t, nodes, 2)
}

func TestListAllClustersReturnsNames(t *testing.T) {
	t.Parallel()

	other := &containerpb.Cluster{Name: "other", Location: "us-central1"}
	fake := &fakeClusterManager{clusters: []*containerpb.Cluster{demoCluster(), other}}
	prov := newProvider(t, fake, "")

	names, err := prov.ListAllClusters(t.Context())

	require.NoError(t, err)
	assert.Equal(t, []string{"demo", "other"}, names)
}

func TestListAllClustersPassesThroughError(t *testing.T) {
	t.Parallel()

	prov := newProvider(t, &fakeClusterManager{listErr: errBoom}, "")

	_, err := prov.ListAllClusters(t.Context())

	require.ErrorIs(t, err, errBoom)
}

func TestNodesExist(t *testing.T) {
	t.Parallel()

	fake := &fakeClusterManager{clusters: []*containerpb.Cluster{
		demoCluster(),
		{Name: "empty", Location: "europe-north1"},
	}}
	prov := newProvider(t, fake, "europe-north1")

	exists, err := prov.NodesExist(t.Context(), "demo")
	require.NoError(t, err)
	assert.True(t, exists)

	exists, err = prov.NodesExist(t.Context(), "empty")
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestStopNodesResizesAllPoolsToZero(t *testing.T) {
	t.Parallel()

	fake := &fakeClusterManager{clusters: []*containerpb.Cluster{demoCluster()}}
	prov := newProvider(t, fake, "europe-north1")

	err := prov.StopNodes(t.Context(), "demo")

	require.NoError(t, err)
	require.Len(t, fake.resizes, 2)
	assert.Equal(t, resizeCall{
		name:  "projects/proj/locations/europe-north1/clusters/demo/nodePools/default-pool",
		count: 0,
	}, fake.resizes[0])
	assert.Equal(t, int32(0), fake.resizes[1].count)
}

func TestStartNodesRestoresInitialCountFlooredAtOne(t *testing.T) {
	t.Parallel()

	fake := &fakeClusterManager{clusters: []*containerpb.Cluster{demoCluster()}}
	prov := newProvider(t, fake, "europe-north1")

	err := prov.StartNodes(t.Context(), "demo")

	require.NoError(t, err)
	require.Len(t, fake.resizes, 2)
	// default-pool restores its configured initial count.
	assert.Equal(t, int32(3), fake.resizes[0].count)
	// burst-pool has no initial count, so the floor of one node applies.
	assert.Equal(t, int32(1), fake.resizes[1].count)
}

func TestStartStopNodesRequirePools(t *testing.T) {
	t.Parallel()

	fake := &fakeClusterManager{clusters: []*containerpb.Cluster{
		{Name: "empty", Location: "europe-north1"},
	}}
	prov := newProvider(t, fake, "europe-north1")

	require.ErrorIs(t, prov.StartNodes(t.Context(), "empty"), provider.ErrNoNodes)
	require.ErrorIs(t, prov.StopNodes(t.Context(), "empty"), provider.ErrNoNodes)
}

func TestStopNodesWrapsResizeError(t *testing.T) {
	t.Parallel()

	fake := &fakeClusterManager{
		clusters: []*containerpb.Cluster{demoCluster()},
		sizeErr:  errBoom,
	}
	prov := newProvider(t, fake, "europe-north1")

	err := prov.StopNodes(t.Context(), "demo")

	require.ErrorIs(t, err, errBoom)
	assert.Contains(t, err.Error(), "stop nodes: resize node pool default-pool")
}

func TestDeleteNodesIsANoOp(t *testing.T) {
	t.Parallel()

	fake := &fakeClusterManager{}
	prov := newProvider(t, fake, "europe-north1")

	require.NoError(t, prov.DeleteNodes(t.Context(), "demo"))
	assert.Empty(t, fake.resizes)
}

func TestGetClusterStatusAggregatesPools(t *testing.T) {
	t.Parallel()

	fake := &fakeClusterManager{clusters: []*containerpb.Cluster{demoCluster()}}
	prov := newProvider(t, fake, "europe-north1")

	status, err := prov.GetClusterStatus(t.Context(), "demo")

	require.NoError(t, err)
	// One of two pools is RUNNING, so the cluster is degraded, not ready.
	assert.Equal(t, provider.PhaseDegraded, status.Phase)
	assert.False(t, status.Ready)
	assert.Equal(t, 2, status.NodesTotal)
	assert.Equal(t, 1, status.NodesReady)
	assert.Equal(t, "34.88.0.1", status.Endpoint)
}

func TestGetClusterStatusNotFound(t *testing.T) {
	t.Parallel()

	prov := newProvider(t, &fakeClusterManager{}, "europe-north1")

	_, err := prov.GetClusterStatus(t.Context(), "missing")

	require.ErrorIs(t, err, provider.ErrClusterNotFound)
}

func TestGetClusterStatusWithoutPoolsIsStopped(t *testing.T) {
	t.Parallel()

	fake := &fakeClusterManager{clusters: []*containerpb.Cluster{
		{Name: "empty", Location: "europe-north1", Endpoint: "34.88.0.2"},
	}}
	prov := newProvider(t, fake, "europe-north1")

	status, err := prov.GetClusterStatus(t.Context(), "empty")

	require.NoError(t, err)
	assert.Equal(t, provider.PhaseStopped, status.Phase)
	assert.False(t, status.Ready)
	assert.Empty(t, status.Nodes)
	assert.Equal(t, "34.88.0.2", status.Endpoint)
}

func TestProjectAccessor(t *testing.T) {
	t.Parallel()

	prov := newProvider(t, &fakeClusterManager{}, "")

	assert.Equal(t, "proj", prov.Project())
}
