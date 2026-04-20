package provider_test

import (
	"context"
	"errors"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provider"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// errStatusFailed is a test error for status operations.
var (
	errStatusFailed              = errors.New("status failed")
	errProviderConnectionRefused = errors.New("connection refused")
	errProviderTimeout           = errors.New("timeout")
	errProviderNetworkError      = errors.New("network error")
)

func TestMockProvider_GetClusterStatus_Success(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	clusterName := testClusterName

	expectedStatus := &provider.ClusterStatus{
		Phase:      "running",
		Ready:      true,
		NodesTotal: 2,
		NodesReady: 2,
		Nodes: []provider.NodeInfo{
			{Name: "node1", ClusterName: clusterName, Role: "control-plane", State: "running"},
			{Name: "node2", ClusterName: clusterName, Role: "worker", State: "running"},
		},
	}

	mockProv := provider.NewMockProvider()
	mockProv.On("GetClusterStatus", ctx, clusterName).Return(expectedStatus, nil)

	status, err := mockProv.GetClusterStatus(ctx, clusterName)

	require.NoError(t, err)
	assert.Equal(t, expectedStatus, status)
	mockProv.AssertExpectations(t)
}

func TestMockProvider_GetClusterStatus_Error(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	clusterName := testClusterName

	mockProv := provider.NewMockProvider()
	mockProv.On("GetClusterStatus", ctx, clusterName).Return(nil, errStatusFailed)

	status, err := mockProv.GetClusterStatus(ctx, clusterName)

	require.Error(t, err)
	require.ErrorIs(t, err, errStatusFailed)
	assert.Nil(t, status)
	mockProv.AssertExpectations(t)
}

func TestMockProvider_GetClusterStatus_NilResult(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	clusterName := testClusterName

	mockProv := provider.NewMockProvider()
	mockProv.On("GetClusterStatus", ctx, clusterName).Return(nil, nil)

	status, err := mockProv.GetClusterStatus(ctx, clusterName)

	require.NoError(t, err)
	assert.Nil(t, status)
	mockProv.AssertExpectations(t)
}

func TestMockProvider_ListNodes_Error(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	clusterName := testClusterName

	mockProv := provider.NewMockProvider()
	mockProv.On("ListNodes", ctx, clusterName).Return(nil, errListFailed)

	nodes, err := mockProv.ListNodes(ctx, clusterName)

	require.Error(t, err)
	assert.Nil(t, nodes)
	mockProv.AssertExpectations(t)
}

func TestMockProvider_ListAllClusters_Error(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	mockProv := provider.NewMockProvider()
	mockProv.On("ListAllClusters", ctx).Return(nil, errListFailed)

	clusters, err := mockProv.ListAllClusters(ctx)

	require.Error(t, err)
	assert.Nil(t, clusters)
	mockProv.AssertExpectations(t)
}

func TestMockProvider_NodesExist_Error(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	clusterName := testClusterName

	mockProv := provider.NewMockProvider()
	mockProv.On("NodesExist", ctx, clusterName).Return(false, errListFailed)

	exists, err := mockProv.NodesExist(ctx, clusterName)

	require.Error(t, err)
	assert.False(t, exists)
	mockProv.AssertExpectations(t)
}

func TestGetClusterStatusFromLister_ClusterNotFoundError(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	clusterName := testClusterName

	mockLister := new(mockNodeLister)
	mockLister.On("ListNodes", ctx, clusterName).Return([]provider.NodeInfo{}, nil)

	status, err := provider.GetClusterStatusFromLister(ctx, mockLister, clusterName, "running")

	require.Error(t, err)
	require.ErrorIs(t, err, provider.ErrClusterNotFound)
	assert.Contains(t, err.Error(), clusterName)
	assert.Nil(t, status)
	mockLister.AssertExpectations(t)
}

func TestGetClusterStatusFromLister_DegradedCluster(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	clusterName := testClusterName

	nodes := []provider.NodeInfo{
		{Name: "node1", ClusterName: clusterName, Role: "control-plane", State: "running"},
		{Name: "node2", ClusterName: clusterName, Role: "worker", State: "stopped"},
		{Name: "node3", ClusterName: clusterName, Role: "worker", State: "running"},
	}

	mockLister := new(mockNodeLister)
	mockLister.On("ListNodes", ctx, clusterName).Return(nodes, nil)

	status, err := provider.GetClusterStatusFromLister(ctx, mockLister, clusterName, "running")

	require.NoError(t, err)
	require.NotNil(t, status)
	assert.Equal(t, "degraded", status.Phase)
	assert.False(t, status.Ready)
	assert.Equal(t, 3, status.NodesTotal)
	assert.Equal(t, 2, status.NodesReady)
	mockLister.AssertExpectations(t)
}

func TestGetClusterStatusFromLister_StoppedCluster(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	clusterName := testClusterName

	nodes := []provider.NodeInfo{
		{Name: "node1", ClusterName: clusterName, Role: "control-plane", State: "stopped"},
		{Name: "node2", ClusterName: clusterName, Role: "worker", State: "stopped"},
	}

	mockLister := new(mockNodeLister)
	mockLister.On("ListNodes", ctx, clusterName).Return(nodes, nil)

	status, err := provider.GetClusterStatusFromLister(ctx, mockLister, clusterName, "running")

	require.NoError(t, err)
	require.NotNil(t, status)
	assert.Equal(t, "stopped", status.Phase)
	assert.False(t, status.Ready)
	assert.Equal(t, 2, status.NodesTotal)
	assert.Equal(t, 0, status.NodesReady)
	mockLister.AssertExpectations(t)
}

func TestBuildClusterStatus_SingleStoppedNode(t *testing.T) {
	t.Parallel()

	nodes := []provider.NodeInfo{
		{Name: "node1", ClusterName: testClusterName, Role: "control-plane", State: "stopped"},
	}

	status := provider.BuildClusterStatus(nodes, "running")

	require.NotNil(t, status)
	assert.Equal(t, "stopped", status.Phase)
	assert.False(t, status.Ready)
	assert.Equal(t, 1, status.NodesTotal)
	assert.Equal(t, 0, status.NodesReady)
}

func TestBuildClusterStatus_CustomReadyState(t *testing.T) {
	t.Parallel()

	nodes := []provider.NodeInfo{
		{Name: "node1", ClusterName: testClusterName, State: "ACTIVE"},
		{Name: "node2", ClusterName: testClusterName, State: "ACTIVE"},
	}

	status := provider.BuildClusterStatus(nodes, "ACTIVE")

	require.NotNil(t, status)
	assert.Equal(t, "ACTIVE", status.Phase)
	assert.True(t, status.Ready)
	assert.Equal(t, 2, status.NodesTotal)
	assert.Equal(t, 2, status.NodesReady)
}

func TestBuildClusterStatus_MixedStatesCustomReady(t *testing.T) {
	t.Parallel()

	nodes := []provider.NodeInfo{
		{Name: "node1", ClusterName: testClusterName, State: "READY"},
		{Name: "node2", ClusterName: testClusterName, State: "PENDING"},
		{Name: "node3", ClusterName: testClusterName, State: "READY"},
		{Name: "node4", ClusterName: testClusterName, State: "FAILED"},
	}

	status := provider.BuildClusterStatus(nodes, "READY")

	require.NotNil(t, status)
	assert.Equal(t, "degraded", status.Phase)
	assert.False(t, status.Ready)
	assert.Equal(t, 4, status.NodesTotal)
	assert.Equal(t, 2, status.NodesReady)
}

func TestCheckNodesExist_NilNodes(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	mockLister := new(mockNodeLister)
	mockLister.On("ListNodes", ctx, testClusterName).Return([]provider.NodeInfo(nil), nil)

	exists, err := provider.CheckNodesExist(ctx, mockLister, testClusterName)

	require.NoError(t, err)
	assert.False(t, exists)
	mockLister.AssertExpectations(t)
}

func TestClusterStatus_Fields(t *testing.T) {
	t.Parallel()

	status := provider.ClusterStatus{
		Phase:      "running",
		Ready:      true,
		NodesTotal: 3,
		NodesReady: 3,
		Nodes: []provider.NodeInfo{
			{Name: "n1"},
			{Name: "n2"},
			{Name: "n3"},
		},
		Endpoint: "https://api.example.com",
	}

	assert.Equal(t, "running", status.Phase)
	assert.True(t, status.Ready)
	assert.Equal(t, 3, status.NodesTotal)
	assert.Equal(t, 3, status.NodesReady)
	assert.Len(t, status.Nodes, 3)
	assert.Equal(t, "https://api.example.com", status.Endpoint)
}

// mockAvailableProviderWithError is a helper to verify error wrapping.
type mockAvailableProviderWithError struct {
	mock.Mock
}

func (m *mockAvailableProviderWithError) IsAvailable() bool {
	return m.Called().Bool(0)
}

func (m *mockAvailableProviderWithError) ListNodes(
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

func TestEnsureAvailableAndListNodes_ErrorWrapping(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	clusterName := testClusterName
	innerErr := errProviderConnectionRefused

	mockProv := new(mockAvailableProviderWithError)
	mockProv.On("IsAvailable").Return(true)
	mockProv.On("ListNodes", ctx, clusterName).Return(nil, innerErr)

	_, err := provider.EnsureAvailableAndListNodes(ctx, mockProv, clusterName)

	require.Error(t, err)
	require.ErrorIs(t, err, innerErr, "inner error should be wrapped")
	assert.Contains(t, err.Error(), "failed to list nodes")
	mockProv.AssertExpectations(t)
}

func TestGetClusterStatusFromLister_ErrorWrapping(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	clusterName := testClusterName
	innerErr := errProviderTimeout

	mockLister := new(mockNodeLister)
	mockLister.On("ListNodes", ctx, clusterName).Return(nil, innerErr)

	_, err := provider.GetClusterStatusFromLister(ctx, mockLister, clusterName, "running")

	require.Error(t, err)
	require.ErrorIs(t, err, innerErr, "inner error should be wrapped")
	assert.Contains(t, err.Error(), "get cluster status")
	mockLister.AssertExpectations(t)
}

func TestCheckNodesExist_ErrorWrapping(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	innerErr := errProviderNetworkError

	mockLister := new(mockNodeLister)
	mockLister.On("ListNodes", ctx, testClusterName).Return(nil, innerErr)

	_, err := provider.CheckNodesExist(ctx, mockLister, testClusterName)

	require.Error(t, err)
	require.ErrorIs(t, err, innerErr, "inner error should be wrapped")
	assert.Contains(t, err.Error(), "check nodes exist")
	mockLister.AssertExpectations(t)
}
