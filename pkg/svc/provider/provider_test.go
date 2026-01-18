package provider_test

import (
	"context"
	"errors"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/svc/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// errListFailed is a test error for list operations.
var errListFailed = errors.New("list failed")

// testClusterName is a constant for test cluster name.
const testClusterName = "test-cluster"

// mockAvailableProvider implements AvailableProvider for testing.
type mockAvailableProvider struct {
	mock.Mock
}

func (m *mockAvailableProvider) IsAvailable() bool {
	args := m.Called()

	return args.Bool(0)
}

func (m *mockAvailableProvider) ListNodes(
	ctx context.Context,
	clusterName string,
) ([]provider.NodeInfo, error) {
	args := m.Called(ctx, clusterName)

	result, ok := args.Get(0).([]provider.NodeInfo)
	if !ok {
		return nil, args.Error(1) //nolint:wrapcheck // mock
	}

	return result, args.Error(1) //nolint:wrapcheck // mock
}

func TestEnsureAvailableAndListNodes_ProviderUnavailable(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	clusterName := testClusterName

	mockProv := new(mockAvailableProvider)
	mockProv.On("IsAvailable").Return(false)

	nodes, err := provider.EnsureAvailableAndListNodes(ctx, mockProv, clusterName)

	require.Error(t, err)
	require.ErrorIs(t, err, provider.ErrProviderUnavailable)
	assert.Nil(t, nodes)
	mockProv.AssertExpectations(t)
}

func TestEnsureAvailableAndListNodes_ListNodesFails(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	clusterName := testClusterName

	mockProv := new(mockAvailableProvider)
	mockProv.On("IsAvailable").Return(true)
	mockProv.On("ListNodes", ctx, clusterName).Return(nil, errListFailed)

	nodes, err := provider.EnsureAvailableAndListNodes(ctx, mockProv, clusterName)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to list nodes")
	assert.Nil(t, nodes)
	mockProv.AssertExpectations(t)
}

func TestEnsureAvailableAndListNodes_ReturnsNodes(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	clusterName := testClusterName
	expectedNodes := []provider.NodeInfo{
		{Name: "node1", ClusterName: clusterName, Role: "control-plane", State: "running"},
		{Name: "node2", ClusterName: clusterName, Role: "worker", State: "running"},
	}

	mockProv := new(mockAvailableProvider)
	mockProv.On("IsAvailable").Return(true)
	mockProv.On("ListNodes", ctx, clusterName).Return(expectedNodes, nil)

	nodes, err := provider.EnsureAvailableAndListNodes(ctx, mockProv, clusterName)

	require.NoError(t, err)
	assert.Equal(t, expectedNodes, nodes)
	mockProv.AssertExpectations(t)
}

func TestEnsureAvailableAndListNodes_ReturnsEmptySlice(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	clusterName := testClusterName

	mockProv := new(mockAvailableProvider)
	mockProv.On("IsAvailable").Return(true)
	mockProv.On("ListNodes", ctx, clusterName).Return([]provider.NodeInfo{}, nil)

	nodes, err := provider.EnsureAvailableAndListNodes(ctx, mockProv, clusterName)

	require.NoError(t, err)
	assert.Empty(t, nodes)
	mockProv.AssertExpectations(t)
}

func TestNodeInfo(t *testing.T) {
	t.Parallel()

	node := provider.NodeInfo{
		Name:        "test-node",
		ClusterName: testClusterName,
		Role:        "control-plane",
		State:       "running",
	}

	assert.Equal(t, "test-node", node.Name)
	assert.Equal(t, testClusterName, node.ClusterName)
	assert.Equal(t, "control-plane", node.Role)
	assert.Equal(t, "running", node.State)
}

func TestErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		err      error
		expected string
	}{
		{"ErrNoNodes", provider.ErrNoNodes, "no nodes found for cluster"},
		{"ErrProviderUnavailable", provider.ErrProviderUnavailable, "provider is not available"},
		{"ErrUnknownLabelScheme", provider.ErrUnknownLabelScheme, "unknown label scheme"},
		{"ErrSkipAction", provider.ErrSkipAction, "skip action"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.expected, testCase.err.Error())
		})
	}
}

func TestMockProvider_StartNodes(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	clusterName := testClusterName

	mockProv := provider.NewMockProvider()
	mockProv.On("StartNodes", ctx, clusterName).Return(nil)

	err := mockProv.StartNodes(ctx, clusterName)

	require.NoError(t, err)
	mockProv.AssertExpectations(t)
}

func TestMockProvider_StopNodes(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	clusterName := testClusterName

	mockProv := provider.NewMockProvider()
	mockProv.On("StopNodes", ctx, clusterName).Return(nil)

	err := mockProv.StopNodes(ctx, clusterName)

	require.NoError(t, err)
	mockProv.AssertExpectations(t)
}

func TestMockProvider_ListNodes(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	clusterName := testClusterName
	expectedNodes := []provider.NodeInfo{
		{Name: "node1", ClusterName: clusterName},
	}

	mockProv := provider.NewMockProvider()
	mockProv.On("ListNodes", ctx, clusterName).Return(expectedNodes, nil)

	nodes, err := mockProv.ListNodes(ctx, clusterName)

	require.NoError(t, err)
	assert.Equal(t, expectedNodes, nodes)
	mockProv.AssertExpectations(t)
}

func TestMockProvider_ListAllClusters(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	expectedClusters := []string{"cluster1", "cluster2"}

	mockProv := provider.NewMockProvider()
	mockProv.On("ListAllClusters", ctx).Return(expectedClusters, nil)

	clusters, err := mockProv.ListAllClusters(ctx)

	require.NoError(t, err)
	assert.Equal(t, expectedClusters, clusters)
	mockProv.AssertExpectations(t)
}

func TestMockProvider_NodesExist(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	clusterName := testClusterName

	mockProv := provider.NewMockProvider()
	mockProv.On("NodesExist", ctx, clusterName).Return(true, nil)

	exists, err := mockProv.NodesExist(ctx, clusterName)

	require.NoError(t, err)
	assert.True(t, exists)
	mockProv.AssertExpectations(t)
}

func TestMockProvider_DeleteNodes(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	clusterName := testClusterName

	mockProv := provider.NewMockProvider()
	mockProv.On("DeleteNodes", ctx, clusterName).Return(nil)

	err := mockProv.DeleteNodes(ctx, clusterName)

	require.NoError(t, err)
	mockProv.AssertExpectations(t)
}
