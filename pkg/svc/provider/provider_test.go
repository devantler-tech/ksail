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
		{"ErrClusterNotFound", provider.ErrClusterNotFound, "cluster not found"},
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

var buildClusterStatusTests = []struct { //nolint:gochecknoglobals // table-driven test cases
	name       string
	nodes      []provider.NodeInfo
	readyState string
	want       *provider.ClusterStatus
}{
	{
		name:       "empty nodes returns nil",
		nodes:      []provider.NodeInfo{},
		readyState: "running",
		want:       nil,
	},
	{
		name:       "nil nodes returns nil",
		nodes:      nil,
		readyState: "running",
		want:       nil,
	},
	{
		name: "all nodes ready",
		nodes: []provider.NodeInfo{
			{Name: "node1", ClusterName: testClusterName, Role: "control-plane", State: "running"},
			{Name: "node2", ClusterName: testClusterName, Role: "worker", State: "running"},
		},
		readyState: "running",
		want: &provider.ClusterStatus{
			Phase:      "running",
			Ready:      true,
			NodesTotal: 2,
			NodesReady: 2,
			Nodes: []provider.NodeInfo{
				{
					Name:        "node1",
					ClusterName: testClusterName,
					Role:        "control-plane",
					State:       "running",
				},
				{Name: "node2", ClusterName: testClusterName, Role: "worker", State: "running"},
			},
		},
	},
	{
		name: "no nodes ready returns stopped phase",
		nodes: []provider.NodeInfo{
			{Name: "node1", ClusterName: testClusterName, Role: "control-plane", State: "stopped"},
			{Name: "node2", ClusterName: testClusterName, Role: "worker", State: "stopped"},
		},
		readyState: "running",
		want: &provider.ClusterStatus{
			Phase:      "stopped",
			Ready:      false,
			NodesTotal: 2,
			NodesReady: 0,
			Nodes: []provider.NodeInfo{
				{
					Name:        "node1",
					ClusterName: testClusterName,
					Role:        "control-plane",
					State:       "stopped",
				},
				{Name: "node2", ClusterName: testClusterName, Role: "worker", State: "stopped"},
			},
		},
	},
	{
		name: "partial nodes ready returns degraded phase",
		nodes: []provider.NodeInfo{
			{Name: "node1", ClusterName: testClusterName, Role: "control-plane", State: "running"},
			{Name: "node2", ClusterName: testClusterName, Role: "worker", State: "stopped"},
		},
		readyState: "running",
		want: &provider.ClusterStatus{
			Phase:      "degraded",
			Ready:      false,
			NodesTotal: 2,
			NodesReady: 1,
			Nodes: []provider.NodeInfo{
				{
					Name:        "node1",
					ClusterName: testClusterName,
					Role:        "control-plane",
					State:       "running",
				},
				{Name: "node2", ClusterName: testClusterName, Role: "worker", State: "stopped"},
			},
		},
	},
	{
		name: "single ready node",
		nodes: []provider.NodeInfo{
			{Name: "node1", ClusterName: testClusterName, Role: "control-plane", State: "RUNNING"},
		},
		readyState: "RUNNING",
		want: &provider.ClusterStatus{
			Phase:      "RUNNING",
			Ready:      true,
			NodesTotal: 1,
			NodesReady: 1,
			Nodes: []provider.NodeInfo{
				{
					Name:        "node1",
					ClusterName: testClusterName,
					Role:        "control-plane",
					State:       "RUNNING",
				},
			},
		},
	},
}

func TestBuildClusterStatus(t *testing.T) {
	t.Parallel()

	for _, testCase := range buildClusterStatusTests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := provider.BuildClusterStatus(testCase.nodes, testCase.readyState)
			assert.Equal(t, testCase.want, got)
		})
	}
}

// mockNodeLister implements NodeLister for testing CheckNodesExist and
// GetClusterStatusFromLister.
type mockNodeLister struct {
	mock.Mock
}

func (m *mockNodeLister) ListNodes(
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

func TestCheckNodesExist(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		nodes       []provider.NodeInfo
		listErr     error
		wantExists  bool
		wantErr     bool
		errContains string
	}{
		{
			name:       "nodes exist returns true",
			nodes:      []provider.NodeInfo{{Name: "node1", ClusterName: testClusterName}},
			wantExists: true,
		},
		{
			name:       "no nodes returns false",
			nodes:      []provider.NodeInfo{},
			wantExists: false,
		},
		{
			name:        "list error propagates",
			listErr:     errListFailed,
			wantErr:     true,
			errContains: "check nodes exist",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			mockLister := new(mockNodeLister)
			mockLister.On("ListNodes", ctx, testClusterName).
				Return(testCase.nodes, testCase.listErr)

			exists, err := provider.CheckNodesExist(ctx, mockLister, testClusterName)

			if testCase.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), testCase.errContains)
			} else {
				require.NoError(t, err)
				assert.Equal(t, testCase.wantExists, exists)
			}

			mockLister.AssertExpectations(t)
		})
	}
}

var getClusterStatusFromListerTests = []struct { //nolint:gochecknoglobals // table-driven test cases
	name        string
	nodes       []provider.NodeInfo
	listErr     error
	readyState  string
	wantPhase   string
	wantReady   bool
	wantErr     bool
	errContains string
}{
	{
		name: "returns status for running cluster",
		nodes: []provider.NodeInfo{
			{Name: "node1", ClusterName: testClusterName, State: "running"},
		},
		readyState: "running",
		wantPhase:  "running",
		wantReady:  true,
	},
	{
		name:        "list error propagates",
		listErr:     errListFailed,
		readyState:  "running",
		wantErr:     true,
		errContains: "get cluster status",
	},
	{
		name:        "empty node list returns cluster not found error",
		nodes:       []provider.NodeInfo{},
		readyState:  "running",
		wantErr:     true,
		errContains: testClusterName,
	},
}

func TestGetClusterStatusFromLister(t *testing.T) {
	t.Parallel()

	for _, testCase := range getClusterStatusFromListerTests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			mockLister := new(mockNodeLister)
			mockLister.On("ListNodes", ctx, testClusterName).
				Return(testCase.nodes, testCase.listErr)

			status, err := provider.GetClusterStatusFromLister(
				ctx,
				mockLister,
				testClusterName,
				testCase.readyState,
			)

			if testCase.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), testCase.errContains)
				assert.Nil(t, status)
			} else {
				require.NoError(t, err)
				require.NotNil(t, status)
				assert.Equal(t, testCase.wantPhase, status.Phase)
				assert.Equal(t, testCase.wantReady, status.Ready)
			}

			mockLister.AssertExpectations(t)
		})
	}
}
