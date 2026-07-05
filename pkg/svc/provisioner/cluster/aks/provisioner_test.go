package aksprovisioner_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	armcontainerservice "github.com/Azure/azure-sdk-for-go/sdk/resourcemanager/containerservice/armcontainerservice/v7"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider"
	aksprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/aks"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clustererr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testClusterName  = "dev"
	testGroup        = "rg-dev"
	testSubscription = "00000000-0000-0000-0000-000000000000"
)

// errBoom is the sentinel the fakes fail with.
var errBoom = errors.New("boom")

// createCall records one CreateCluster invocation the provisioner made.
type createCall struct {
	resourceGroup string
	name          string
	cluster       armcontainerservice.ManagedCluster
}

// deleteCall records one DeleteCluster invocation the provisioner made.
type deleteCall struct {
	resourceGroup string
	name          string
}

// fakeClusterClient implements the provisioner's ClusterClient seam with
// injectable behaviour per operation, recording every call it receives.
type fakeClusterClient struct {
	creates []createCall
	deletes []deleteCall
	lists   []string

	createErr error
	deleteErr error
	listErr   error

	clusters []*armcontainerservice.ManagedCluster
}

func (f *fakeClusterClient) CreateCluster(
	_ context.Context,
	resourceGroup, name string,
	cluster armcontainerservice.ManagedCluster,
) (armcontainerservice.ManagedCluster, error) {
	f.creates = append(f.creates, createCall{
		resourceGroup: resourceGroup,
		name:          name,
		cluster:       cluster,
	})

	return cluster, f.createErr
}

func (f *fakeClusterClient) DeleteCluster(
	_ context.Context,
	resourceGroup, name string,
) error {
	f.deletes = append(f.deletes, deleteCall{resourceGroup: resourceGroup, name: name})

	return f.deleteErr
}

func (f *fakeClusterClient) ListClusters(
	_ context.Context,
	resourceGroup string,
) ([]*armcontainerservice.ManagedCluster, error) {
	f.lists = append(f.lists, resourceGroup)

	return f.clusters, f.listErr
}

// fakeProvider records Start/Stop delegation without touching real
// infrastructure. The embedded interface panics on anything not overridden,
// pinning that the provisioner only uses StartNodes/StopNodes.
type fakeProvider struct {
	provider.Provider

	started  []string
	stopped  []string
	startErr error
	stopErr  error
}

func (f *fakeProvider) StartNodes(_ context.Context, clusterName string) error {
	f.started = append(f.started, clusterName)

	return f.startErr
}

func (f *fakeProvider) StopNodes(_ context.Context, clusterName string) error {
	f.stopped = append(f.stopped, clusterName)

	return f.stopErr
}

// managedCluster assembles a ManagedCluster carrying the ARM ID the
// resource-group resolution parses.
func managedCluster(name, resourceGroup string) *armcontainerservice.ManagedCluster {
	armID := fmt.Sprintf(
		"/subscriptions/%s/resourceGroups/%s/providers/Microsoft.ContainerService/managedClusters/%s",
		testSubscription,
		resourceGroup,
		name,
	)

	return &armcontainerservice.ManagedCluster{
		ID:   new(armID),
		Name: new(name),
	}
}

// newProvisioner wires a Provisioner around the fake client, failing the test
// on construction errors.
func newProvisioner(
	t *testing.T,
	fake *fakeClusterClient,
	resourceGroup string,
	clusterSpec *armcontainerservice.ManagedCluster,
	infra provider.Provider,
) *aksprovisioner.Provisioner {
	t.Helper()

	provisioner, err := aksprovisioner.NewProvisioner(
		testClusterName, resourceGroup, clusterSpec, fake, infra,
	)
	require.NoError(t, err)

	return provisioner
}

func TestNewProvisioner_RequiresClient(t *testing.T) {
	t.Parallel()

	_, err := aksprovisioner.NewProvisioner(testClusterName, testGroup, nil, nil, nil)

	require.ErrorIs(t, err, aksprovisioner.ErrClientRequired)
}

func TestCreate_RequiresClusterSpec(t *testing.T) {
	t.Parallel()

	fake := &fakeClusterClient{}
	provisioner := newProvisioner(t, fake, testGroup, nil, nil)

	err := provisioner.Create(t.Context(), testClusterName)

	require.ErrorIs(t, err, aksprovisioner.ErrClusterSpecRequired)
	assert.Empty(t, fake.creates)
}

func TestCreate_RequiresResourceGroup(t *testing.T) {
	t.Parallel()

	fake := &fakeClusterClient{}
	provisioner := newProvisioner(
		t, fake, "", &armcontainerservice.ManagedCluster{}, nil,
	)

	err := provisioner.Create(t.Context(), testClusterName)

	require.ErrorIs(t, err, aksprovisioner.ErrResourceGroupRequired)
	assert.Empty(t, fake.creates)
}

func TestCreate_RequiresName(t *testing.T) {
	t.Parallel()

	fake := &fakeClusterClient{}
	provisioner, err := aksprovisioner.NewProvisioner(
		"", testGroup, &armcontainerservice.ManagedCluster{}, fake, nil,
	)
	require.NoError(t, err)

	err = provisioner.Create(t.Context(), "")

	require.ErrorIs(t, err, aksprovisioner.ErrNameRequired)
	assert.Empty(t, fake.creates)
}

func TestCreate_SubmitsDeclarativeSpec(t *testing.T) {
	t.Parallel()

	location := "westeurope"
	spec := &armcontainerservice.ManagedCluster{Location: new(location)}
	fake := &fakeClusterClient{}
	provisioner := newProvisioner(t, fake, testGroup, spec, nil)

	err := provisioner.Create(t.Context(), "")

	require.NoError(t, err)
	require.Len(t, fake.creates, 1)
	assert.Equal(t, testGroup, fake.creates[0].resourceGroup)
	assert.Equal(t, testClusterName, fake.creates[0].name)
	assert.Equal(t, *spec, fake.creates[0].cluster)
}

func TestCreate_WrapsClientError(t *testing.T) {
	t.Parallel()

	fake := &fakeClusterClient{createErr: errBoom}
	provisioner := newProvisioner(
		t, fake, testGroup, &armcontainerservice.ManagedCluster{}, nil,
	)

	err := provisioner.Create(t.Context(), testClusterName)

	require.ErrorIs(t, err, errBoom)
	assert.ErrorContains(t, err, "aks create cluster")
}

func TestDelete_UsesPinnedResourceGroup(t *testing.T) {
	t.Parallel()

	fake := &fakeClusterClient{}
	provisioner := newProvisioner(t, fake, testGroup, nil, nil)

	err := provisioner.Delete(t.Context(), "")

	require.NoError(t, err)
	require.Len(t, fake.deletes, 1)
	assert.Equal(t, deleteCall{resourceGroup: testGroup, name: testClusterName}, fake.deletes[0])
	assert.Empty(t, fake.lists, "a pinned resource group needs no resolution list")
}

func TestDelete_ResolvesResourceGroupWhenUnpinned(t *testing.T) {
	t.Parallel()

	fake := &fakeClusterClient{
		clusters: []*armcontainerservice.ManagedCluster{
			managedCluster("other", "rg-other"),
			managedCluster(testClusterName, testGroup),
		},
	}
	provisioner := newProvisioner(t, fake, "", nil, nil)

	err := provisioner.Delete(t.Context(), testClusterName)

	require.NoError(t, err)
	require.Equal(t, []string{""}, fake.lists, "resolution lists subscription-wide")
	require.Len(t, fake.deletes, 1)
	assert.Equal(t, deleteCall{resourceGroup: testGroup, name: testClusterName}, fake.deletes[0])
}

func TestDelete_UnknownClusterFailsResolution(t *testing.T) {
	t.Parallel()

	fake := &fakeClusterClient{
		clusters: []*armcontainerservice.ManagedCluster{managedCluster("other", "rg-other")},
	}
	provisioner := newProvisioner(t, fake, "", nil, nil)

	err := provisioner.Delete(t.Context(), testClusterName)

	require.ErrorIs(t, err, aksprovisioner.ErrClusterNotFound)
	assert.Empty(t, fake.deletes)
}

func TestDelete_EmptyTargetFailsFastWithoutCalls(t *testing.T) {
	t.Parallel()

	fake := &fakeClusterClient{}
	provisioner, err := aksprovisioner.NewProvisioner("", "", nil, fake, nil)
	require.NoError(t, err)

	err = provisioner.Delete(t.Context(), "")

	require.ErrorIs(t, err, aksprovisioner.ErrClusterNotFound)
	assert.Empty(t, fake.lists)
	assert.Empty(t, fake.deletes)
}

func TestStart_DelegatesToProvider(t *testing.T) {
	t.Parallel()

	infra := &fakeProvider{}
	provisioner := newProvisioner(t, &fakeClusterClient{}, testGroup, nil, infra)

	err := provisioner.Start(t.Context(), "override")

	require.NoError(t, err)
	assert.Equal(t, []string{"override"}, infra.started)
}

func TestStart_RequiresProvider(t *testing.T) {
	t.Parallel()

	provisioner := newProvisioner(t, &fakeClusterClient{}, testGroup, nil, nil)

	err := provisioner.Start(t.Context(), testClusterName)

	require.ErrorIs(t, err, clustererr.ErrUnsupportedProvider)
}

func TestStop_DelegatesToProvider(t *testing.T) {
	t.Parallel()

	infra := &fakeProvider{}
	provisioner := newProvisioner(t, &fakeClusterClient{}, testGroup, nil, infra)

	err := provisioner.Stop(t.Context(), "")

	require.NoError(t, err)
	assert.Equal(t, []string{testClusterName}, infra.stopped)
}

func TestStop_RequiresProvider(t *testing.T) {
	t.Parallel()

	provisioner := newProvisioner(t, &fakeClusterClient{}, testGroup, nil, nil)

	err := provisioner.Stop(t.Context(), testClusterName)

	require.ErrorIs(t, err, clustererr.ErrUnsupportedProvider)
}

func TestStop_WrapsProviderError(t *testing.T) {
	t.Parallel()

	infra := &fakeProvider{stopErr: errBoom}
	provisioner := newProvisioner(t, &fakeClusterClient{}, testGroup, nil, infra)

	err := provisioner.Stop(t.Context(), testClusterName)

	require.ErrorIs(t, err, errBoom)
	assert.ErrorContains(t, err, "stop nodes")
}

func TestSetProvider_Overrides(t *testing.T) {
	t.Parallel()

	replacement := &fakeProvider{}
	provisioner := newProvisioner(t, &fakeClusterClient{}, testGroup, nil, nil)
	provisioner.SetProvider(replacement)

	err := provisioner.Start(t.Context(), testClusterName)

	require.NoError(t, err)
	assert.Equal(t, []string{testClusterName}, replacement.started)
}

func TestList_ReturnsClusterNamesInConfiguredScope(t *testing.T) {
	t.Parallel()

	fake := &fakeClusterClient{
		clusters: []*armcontainerservice.ManagedCluster{
			managedCluster("alpha", testGroup),
			nil,
			{Name: nil},
			managedCluster("beta", "rg-other"),
		},
	}
	provisioner := newProvisioner(t, fake, "", nil, nil)

	names, err := provisioner.List(t.Context())

	require.NoError(t, err)
	assert.Equal(t, []string{"alpha", "beta"}, names)
	assert.Equal(t, []string{""}, fake.lists, "no pinned group lists subscription-wide")
}

func TestList_WrapsClientError(t *testing.T) {
	t.Parallel()

	fake := &fakeClusterClient{listErr: errBoom}
	provisioner := newProvisioner(t, fake, testGroup, nil, nil)

	_, err := provisioner.List(t.Context())

	require.ErrorIs(t, err, errBoom)
	assert.ErrorContains(t, err, "aks list clusters")
}

func TestExists_ReportsMembership(t *testing.T) {
	t.Parallel()

	fake := &fakeClusterClient{
		clusters: []*armcontainerservice.ManagedCluster{
			managedCluster(testClusterName, testGroup),
		},
	}
	provisioner := newProvisioner(t, fake, testGroup, nil, nil)

	exists, err := provisioner.Exists(t.Context(), "")
	require.NoError(t, err)
	assert.True(t, exists)

	exists, err = provisioner.Exists(t.Context(), "missing")
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestExists_EmptyTargetIsFalseWithoutCalls(t *testing.T) {
	t.Parallel()

	fake := &fakeClusterClient{}
	provisioner, err := aksprovisioner.NewProvisioner("", testGroup, nil, fake, nil)
	require.NoError(t, err)

	exists, err := provisioner.Exists(t.Context(), "")

	require.NoError(t, err)
	assert.False(t, exists)
	assert.Empty(t, fake.lists)
}

func TestExists_WrapsClientError(t *testing.T) {
	t.Parallel()

	fake := &fakeClusterClient{listErr: errBoom}
	provisioner := newProvisioner(t, fake, testGroup, nil, nil)

	_, err := provisioner.Exists(t.Context(), testClusterName)

	require.ErrorIs(t, err, errBoom)
}
