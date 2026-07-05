package azure_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	armcontainerservice "github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/containerservice/armcontainerservice/v7"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/azure"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testCluster       = "dev"
	testResourceGroup = "rg-dev"
)

// resizeCall records one SetAgentPoolCount invocation the provider made.
type resizeCall struct {
	resourceGroup string
	cluster       string
	pool          string
	count         int32
}

// fakeClusterClient scripts the azure.ClusterClient seam: it serves the
// configured clusters for Get/List and records resizes, mirroring the gcp
// provider's fakeClusterManager.
type fakeClusterClient struct {
	clusters []*armcontainerservice.ManagedCluster

	getErr  error
	listErr error
	sizeErr error

	lastGetResourceGroup string
	resizes              []resizeCall
}

func (f *fakeClusterClient) GetCluster(
	_ context.Context,
	resourceGroup, name string,
) (armcontainerservice.ManagedCluster, error) {
	f.lastGetResourceGroup = resourceGroup

	if f.getErr != nil {
		return armcontainerservice.ManagedCluster{}, f.getErr
	}

	for _, cluster := range f.clusters {
		if cluster.Name != nil && *cluster.Name == name {
			return *cluster, nil
		}
	}

	return armcontainerservice.ManagedCluster{}, &azcore.ResponseError{
		StatusCode: http.StatusNotFound,
		ErrorCode:  "ResourceNotFound",
	}
}

func (f *fakeClusterClient) ListClusters(
	_ context.Context,
	_ string,
) ([]*armcontainerservice.ManagedCluster, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}

	return f.clusters, nil
}

func (f *fakeClusterClient) SetAgentPoolCount(
	_ context.Context,
	resourceGroup, clusterName, poolName string,
	count int32,
) error {
	if f.sizeErr != nil {
		return f.sizeErr
	}

	f.resizes = append(f.resizes, resizeCall{
		resourceGroup: resourceGroup,
		cluster:       clusterName,
		pool:          poolName,
		count:         count,
	})

	return nil
}

// newProvider builds a Provider over the fake, failing the test on a
// constructor error.
func newProvider(t *testing.T, fake *fakeClusterClient, resourceGroup string) *azure.Provider {
	t.Helper()

	prov, err := azure.NewProvider(fake, resourceGroup)
	require.NoError(t, err)

	return prov
}

// managedCluster assembles a ManagedCluster with the given name and agent
// pools, carrying the ARM ID the resource-group resolution parses.
func managedCluster(
	name string,
	pools ...*armcontainerservice.ManagedClusterAgentPoolProfile,
) *armcontainerservice.ManagedCluster {
	armID := fmt.Sprintf(
		"/subscriptions/00000000-0000-0000-0000-000000000000"+
			"/resourceGroups/%s/providers/Microsoft.ContainerService/managedClusters/%s",
		testResourceGroup, name,
	)

	return &armcontainerservice.ManagedCluster{
		ID:   new(armID),
		Name: new(name),
		Properties: &armcontainerservice.ManagedClusterProperties{
			Fqdn:              new(name + ".hcp.westeurope.azmk8s.io"),
			AgentPoolProfiles: pools,
		},
	}
}

// agentPool assembles an agent-pool profile in the given provisioning state.
func agentPool(
	name string,
	count int32,
	state string,
) *armcontainerservice.ManagedClusterAgentPoolProfile {
	return &armcontainerservice.ManagedClusterAgentPoolProfile{
		Name:              new(name),
		Count:             new(count),
		VMSize:            new("Standard_D2s_v5"),
		ProvisioningState: new(state),
	}
}

func TestNewProviderRequiresClient(t *testing.T) {
	t.Parallel()

	_, err := azure.NewProvider(nil, testResourceGroup)
	require.ErrorIs(t, err, azure.ErrClientRequired)
}

// TestListNodesCollapsesAgentPools pins the pool→NodeInfo collapse: one entry
// per agent pool carrying the pool name, worker role, provisioning state, and
// VM size — mirroring the gcp provider's node-pool collapse.
func TestListNodesCollapsesAgentPools(t *testing.T) {
	t.Parallel()

	fake := &fakeClusterClient{clusters: []*armcontainerservice.ManagedCluster{
		managedCluster(
			testCluster,
			agentPool("system", 1, "Succeeded"),
			agentPool("user", 3, "Creating"),
		),
	}}
	prov := newProvider(t, fake, testResourceGroup)

	nodes, err := prov.ListNodes(context.Background(), testCluster)
	require.NoError(t, err)
	require.Len(t, nodes, 2)

	assert.Equal(t, provider.NodeInfo{
		Name:        "system",
		ClusterName: testCluster,
		Role:        "worker",
		State:       "Succeeded",
		ServerType:  "Standard_D2s_v5",
	}, nodes[0])
	assert.Equal(t, "user", nodes[1].Name)
	assert.Equal(t, "Creating", nodes[1].State)
}

// TestListNodesResolvesResourceGroupWhenUnconfigured pins the ARM-ID
// resolution: with an empty resource group the provider finds the cluster in
// a subscription-wide list and targets the resource group parsed from its ID.
func TestListNodesResolvesResourceGroupWhenUnconfigured(t *testing.T) {
	t.Parallel()

	fake := &fakeClusterClient{clusters: []*armcontainerservice.ManagedCluster{
		managedCluster(testCluster, agentPool("system", 1, "Succeeded")),
	}}
	prov := newProvider(t, fake, "")

	nodes, err := prov.ListNodes(context.Background(), testCluster)
	require.NoError(t, err)
	require.Len(t, nodes, 1)
	assert.Equal(t, testResourceGroup, fake.lastGetResourceGroup)
}

// TestListNodesUnknownClusterWithoutResourceGroupIsNotFound pins that the
// resolution path classifies a cluster absent from the subscription as
// provider.ErrClusterNotFound before any cluster-scoped call is attempted.
func TestListNodesUnknownClusterWithoutResourceGroupIsNotFound(t *testing.T) {
	t.Parallel()

	prov := newProvider(t, &fakeClusterClient{}, "")

	_, err := prov.ListNodes(context.Background(), testCluster)
	require.ErrorIs(t, err, provider.ErrClusterNotFound)
}

// TestStartNodesRestoresAtLeastOneNode pins the start targets: a stopped pool
// (count zero) is restored to one node, a pool with an autoscaler minimum to
// that minimum, and a running pool has its current size re-asserted — the
// idempotent re-assert the design locked (AKS preserves no creation-time
// count on the profile).
func TestStartNodesRestoresAtLeastOneNode(t *testing.T) {
	t.Parallel()

	autoscaled := agentPool("autoscaled", 0, "Succeeded")
	autoscaled.MinCount = new(int32(3))

	fake := &fakeClusterClient{clusters: []*armcontainerservice.ManagedCluster{
		managedCluster(
			testCluster,
			agentPool("stopped", 0, "Succeeded"),
			autoscaled,
			agentPool("running", 5, "Succeeded"),
		),
	}}
	prov := newProvider(t, fake, testResourceGroup)

	err := prov.StartNodes(context.Background(), testCluster)
	require.NoError(t, err)

	require.Len(t, fake.resizes, 3)
	assert.Equal(t, resizeCall{testResourceGroup, testCluster, "stopped", 1}, fake.resizes[0])
	assert.Equal(t, resizeCall{testResourceGroup, testCluster, "autoscaled", 3}, fake.resizes[1])
	assert.Equal(t, resizeCall{testResourceGroup, testCluster, "running", 5}, fake.resizes[2])
}

// TestStopNodesResizesPoolsToZero pins the stop semantics: every pool is
// resized to zero nodes (the control plane stays up; node costs stop).
func TestStopNodesResizesPoolsToZero(t *testing.T) {
	t.Parallel()

	fake := &fakeClusterClient{clusters: []*armcontainerservice.ManagedCluster{
		managedCluster(
			testCluster,
			agentPool("system", 1, "Succeeded"),
			agentPool("user", 3, "Succeeded"),
		),
	}}
	prov := newProvider(t, fake, testResourceGroup)

	err := prov.StopNodes(context.Background(), testCluster)
	require.NoError(t, err)

	require.Len(t, fake.resizes, 2)

	for _, resize := range fake.resizes {
		assert.Equal(t, int32(0), resize.count)
	}
}

// TestStartStopNodesRequirePools pins that scaling a cluster without agent
// pools is classified as provider.ErrNoNodes rather than silently succeeding.
func TestStartStopNodesRequirePools(t *testing.T) {
	t.Parallel()

	fake := &fakeClusterClient{clusters: []*armcontainerservice.ManagedCluster{
		managedCluster(testCluster),
	}}
	prov := newProvider(t, fake, testResourceGroup)

	require.ErrorIs(t, prov.StartNodes(context.Background(), testCluster), provider.ErrNoNodes)
	require.ErrorIs(t, prov.StopNodes(context.Background(), testCluster), provider.ErrNoNodes)
	assert.Empty(t, fake.resizes)
}

// TestGetClusterStatusAggregatesPools pins the status aggregation: a cluster
// with one ready and one provisioning pool is degraded, counts both, and
// carries the API-server FQDN as its endpoint.
func TestGetClusterStatusAggregatesPools(t *testing.T) {
	t.Parallel()

	fake := &fakeClusterClient{clusters: []*armcontainerservice.ManagedCluster{
		managedCluster(
			testCluster,
			agentPool("system", 1, "Succeeded"),
			agentPool("user", 3, "Creating"),
		),
	}}
	prov := newProvider(t, fake, testResourceGroup)

	status, err := prov.GetClusterStatus(context.Background(), testCluster)
	require.NoError(t, err)
	require.NotNil(t, status)

	assert.Equal(t, provider.PhaseDegraded, status.Phase)
	assert.False(t, status.Ready)
	assert.Equal(t, 2, status.NodesTotal)
	assert.Equal(t, 1, status.NodesReady)
	assert.Equal(t, testCluster+".hcp.westeurope.azmk8s.io", status.Endpoint)
}

// TestGetClusterStatusWithoutPoolsIsStopped pins the zero-pool contract: the
// status is non-nil, stopped, and empty rather than a nil status.
func TestGetClusterStatusWithoutPoolsIsStopped(t *testing.T) {
	t.Parallel()

	fake := &fakeClusterClient{clusters: []*armcontainerservice.ManagedCluster{
		managedCluster(testCluster),
	}}
	prov := newProvider(t, fake, testResourceGroup)

	status, err := prov.GetClusterStatus(context.Background(), testCluster)
	require.NoError(t, err)
	require.NotNil(t, status)

	assert.Equal(t, provider.PhaseStopped, status.Phase)
	assert.False(t, status.Ready)
	assert.Empty(t, status.Nodes)
}

// TestGetClusterStatusNotFoundTranslated pins the error translation: an ARM
// 404 from the client surfaces as provider.ErrClusterNotFound.
func TestGetClusterStatusNotFoundTranslated(t *testing.T) {
	t.Parallel()

	prov := newProvider(t, &fakeClusterClient{}, testResourceGroup)

	_, err := prov.GetClusterStatus(context.Background(), testCluster)
	require.ErrorIs(t, err, provider.ErrClusterNotFound)
}

// TestListAllClustersReturnsNames pins subscription/resource-group listing.
func TestListAllClustersReturnsNames(t *testing.T) {
	t.Parallel()

	fake := &fakeClusterClient{clusters: []*armcontainerservice.ManagedCluster{
		managedCluster("alpha"),
		managedCluster("beta"),
	}}
	prov := newProvider(t, fake, testResourceGroup)

	names, err := prov.ListAllClusters(context.Background())
	require.NoError(t, err)
	assert.Equal(t, []string{"alpha", "beta"}, names)
}

// TestNodesExistReflectsPools pins the NodesExist contract on both sides.
func TestNodesExistReflectsPools(t *testing.T) {
	t.Parallel()

	withPools := &fakeClusterClient{clusters: []*armcontainerservice.ManagedCluster{
		managedCluster(testCluster, agentPool("system", 1, "Succeeded")),
	}}
	exists, err := newProvider(t, withPools, testResourceGroup).
		NodesExist(context.Background(), testCluster)
	require.NoError(t, err)
	assert.True(t, exists)

	withoutPools := &fakeClusterClient{clusters: []*armcontainerservice.ManagedCluster{
		managedCluster(testCluster),
	}}
	exists, err = newProvider(t, withoutPools, testResourceGroup).
		NodesExist(context.Background(), testCluster)
	require.NoError(t, err)
	assert.False(t, exists)
}

// TestDeleteNodesIsNoOp pins that pool deletion is owned by cluster deletion.
func TestDeleteNodesIsNoOp(t *testing.T) {
	t.Parallel()

	fake := &fakeClusterClient{}
	prov := newProvider(t, fake, testResourceGroup)

	require.NoError(t, prov.DeleteNodes(context.Background(), testCluster))
	assert.Empty(t, fake.resizes)
}

// TestResourceGroupAccessor pins the configured-resource-group accessor.
func TestResourceGroupAccessor(t *testing.T) {
	t.Parallel()

	prov := newProvider(t, &fakeClusterClient{}, testResourceGroup)
	assert.Equal(t, testResourceGroup, prov.ResourceGroup())
}
