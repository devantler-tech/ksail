package docker_test

import (
	"context"
	"errors"
	"testing"

	dockerclient "github.com/devantler-tech/ksail/v5/pkg/client/docker"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provider"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provider/docker"
	"github.com/docker/docker/api/types/container"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// testClusterName is used across multiple tests.
const testClusterName = "test-cluster"

// errContainerList simulates a Docker API error.
var errContainerList = errors.New("failed to list containers")

// newKindContainers creates test containers with Kind naming convention.
func newKindContainers(_ string) []container.Summary {
	return []container.Summary{
		{
			ID:    "cp1",
			Names: []string{"/" + testClusterName + "-control-plane"},
			State: "running",
		},
		{
			ID:    "w1",
			Names: []string{"/" + testClusterName + "-worker"},
			State: "running",
		},
	}
}

// newK3dContainers creates test containers with K3d labels.
func newK3dContainers(clusterName string) []container.Summary {
	return []container.Summary{
		{
			ID:    "s1",
			Names: []string{"/" + "k3d-" + clusterName + "-server-0"},
			State: "running",
			Labels: map[string]string{
				docker.LabelK3dCluster: clusterName,
				docker.LabelK3dRole:    "server",
			},
		},
		{
			ID:    "a1",
			Names: []string{"/" + "k3d-" + clusterName + "-agent-0"},
			State: "running",
			Labels: map[string]string{
				docker.LabelK3dCluster: clusterName,
				docker.LabelK3dRole:    "agent",
			},
		},
	}
}

// newTalosContainers creates test containers with Talos labels.
func newTalosContainers(clusterName string) []container.Summary {
	return []container.Summary{
		{
			ID:    "tcp1",
			Names: []string{"/" + clusterName + "-controlplane-1"},
			State: "running",
			Labels: map[string]string{
				docker.LabelTalosOwned:       "true",
				docker.LabelTalosClusterName: clusterName,
				docker.LabelTalosType:        "controlplane",
			},
		},
	}
}

func TestNewProvider(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		scheme docker.LabelScheme
	}{
		{"Kind", docker.LabelSchemeKind},
		{"K3d", docker.LabelSchemeK3d},
		{"Talos", docker.LabelSchemeTalos},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			client := dockerclient.NewMockAPIClient(t)
			prov := docker.NewProvider(client, tc.scheme)

			require.NotNil(t, prov)
			assert.True(t, prov.IsAvailable())
		})
	}
}

func TestProvider_IsAvailable_NilClient(t *testing.T) {
	t.Parallel()

	prov := docker.NewProvider(nil, docker.LabelSchemeKind)

	assert.False(t, prov.IsAvailable())
}

func TestProvider_ListNodes_NilClient(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	prov := docker.NewProvider(nil, docker.LabelSchemeKind)

	nodes, err := prov.ListNodes(ctx, testClusterName)

	require.Error(t, err)
	require.ErrorIs(t, err, provider.ErrProviderUnavailable)
	assert.Nil(t, nodes)
}

func TestProvider_ListNodes_Kind(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	client := dockerclient.NewMockAPIClient(t)
	containers := newKindContainers(testClusterName)

	client.EXPECT().
		ContainerList(ctx, container.ListOptions{All: true}).
		Return(containers, nil)

	prov := docker.NewProvider(client, docker.LabelSchemeKind)

	nodes, err := prov.ListNodes(ctx, testClusterName)

	require.NoError(t, err)
	require.Len(t, nodes, 2)
	assert.Equal(t, testClusterName+"-control-plane", nodes[0].Name)
	assert.Equal(t, "control-plane", nodes[0].Role)
	assert.Equal(t, testClusterName+"-worker", nodes[1].Name)
	assert.Equal(t, "worker", nodes[1].Role)
}

func TestProvider_ListNodes_K3d(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	client := dockerclient.NewMockAPIClient(t)
	containers := newK3dContainers(testClusterName)

	client.EXPECT().
		ContainerList(ctx, mock.MatchedBy(func(opts container.ListOptions) bool {
			return opts.All && opts.Filters.Get("label") != nil
		})).
		Return(containers, nil)

	prov := docker.NewProvider(client, docker.LabelSchemeK3d)

	nodes, err := prov.ListNodes(ctx, testClusterName)

	require.NoError(t, err)
	require.Len(t, nodes, 2)
	assert.Equal(t, "server", nodes[0].Role)
	assert.Equal(t, "agent", nodes[1].Role)
}

func TestProvider_ListNodes_Talos(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	client := dockerclient.NewMockAPIClient(t)
	containers := newTalosContainers(testClusterName)

	client.EXPECT().
		ContainerList(ctx, mock.MatchedBy(func(opts container.ListOptions) bool {
			return opts.All && opts.Filters.Get("label") != nil
		})).
		Return(containers, nil)

	prov := docker.NewProvider(client, docker.LabelSchemeTalos)

	nodes, err := prov.ListNodes(ctx, testClusterName)

	require.NoError(t, err)
	require.Len(t, nodes, 1)
	assert.Equal(t, testClusterName+"-controlplane-1", nodes[0].Name)
	assert.Equal(t, "controlplane", nodes[0].Role)
}

func TestProvider_ListNodes_Error(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	client := dockerclient.NewMockAPIClient(t)

	client.EXPECT().
		ContainerList(ctx, container.ListOptions{All: true}).
		Return(nil, errContainerList)

	prov := docker.NewProvider(client, docker.LabelSchemeKind)

	nodes, err := prov.ListNodes(ctx, testClusterName)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to list containers")
	assert.Nil(t, nodes)
}

func TestProvider_ListNodes_EmptyResult(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	client := dockerclient.NewMockAPIClient(t)

	client.EXPECT().
		ContainerList(ctx, container.ListOptions{All: true}).
		Return([]container.Summary{}, nil)

	prov := docker.NewProvider(client, docker.LabelSchemeKind)

	nodes, err := prov.ListNodes(ctx, testClusterName)

	require.NoError(t, err)
	assert.Empty(t, nodes)
}

func TestProvider_ListAllClusters_NilClient(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	prov := docker.NewProvider(nil, docker.LabelSchemeKind)

	clusters, err := prov.ListAllClusters(ctx)

	require.Error(t, err)
	require.ErrorIs(t, err, provider.ErrProviderUnavailable)
	assert.Nil(t, clusters)
}

func TestProvider_ListAllClusters_Kind(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	client := dockerclient.NewMockAPIClient(t)

	allContainers := []container.Summary{
		{ID: "1", Names: []string{"/cluster1-control-plane"}},
		{ID: "2", Names: []string{"/cluster1-worker"}},
		{ID: "3", Names: []string{"/cluster2-control-plane"}},
		{ID: "4", Names: []string{"/other-container"}},
	}

	client.EXPECT().
		ContainerList(ctx, container.ListOptions{All: true}).
		Return(allContainers, nil)

	prov := docker.NewProvider(client, docker.LabelSchemeKind)

	clusters, err := prov.ListAllClusters(ctx)

	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"cluster1", "cluster2"}, clusters)
}

func TestProvider_ListAllClusters_K3d(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	client := dockerclient.NewMockAPIClient(t)

	allContainers := []container.Summary{
		{
			ID:     "1",
			Names:  []string{"/k3d-cluster1-server"},
			Labels: map[string]string{docker.LabelK3dCluster: "cluster1"},
		},
		{
			ID:     "2",
			Names:  []string{"/k3d-cluster2-server"},
			Labels: map[string]string{docker.LabelK3dCluster: "cluster2"},
		},
	}

	client.EXPECT().
		ContainerList(ctx, container.ListOptions{All: true}).
		Return(allContainers, nil)

	prov := docker.NewProvider(client, docker.LabelSchemeK3d)

	clusters, err := prov.ListAllClusters(ctx)

	require.NoError(t, err)
	assert.ElementsMatch(t, []string{"cluster1", "cluster2"}, clusters)
}

func TestProvider_NodesExist_NilClient(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	prov := docker.NewProvider(nil, docker.LabelSchemeKind)

	exists, err := prov.NodesExist(ctx, testClusterName)

	require.Error(t, err)
	require.ErrorIs(t, err, provider.ErrProviderUnavailable)
	assert.False(t, exists)
}

func TestProvider_NodesExist_True(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	client := dockerclient.NewMockAPIClient(t)
	containers := newKindContainers(testClusterName)

	client.EXPECT().
		ContainerList(ctx, container.ListOptions{All: true}).
		Return(containers, nil)

	prov := docker.NewProvider(client, docker.LabelSchemeKind)

	exists, err := prov.NodesExist(ctx, testClusterName)

	require.NoError(t, err)
	assert.True(t, exists)
}

func TestProvider_NodesExist_False(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	client := dockerclient.NewMockAPIClient(t)

	client.EXPECT().
		ContainerList(ctx, container.ListOptions{All: true}).
		Return([]container.Summary{}, nil)

	prov := docker.NewProvider(client, docker.LabelSchemeKind)

	exists, err := prov.NodesExist(ctx, testClusterName)

	require.NoError(t, err)
	assert.False(t, exists)
}

func TestProvider_StartNodes_NilClient(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	prov := docker.NewProvider(nil, docker.LabelSchemeKind)

	err := prov.StartNodes(ctx, testClusterName)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "provider is not available")
}

func TestProvider_StartNodes_NoNodes(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	client := dockerclient.NewMockAPIClient(t)

	client.EXPECT().
		ContainerList(ctx, container.ListOptions{All: true}).
		Return([]container.Summary{}, nil)

	prov := docker.NewProvider(client, docker.LabelSchemeKind)

	err := prov.StartNodes(ctx, testClusterName)

	require.Error(t, err)
	require.ErrorIs(t, err, provider.ErrNoNodes)
}

func TestProvider_StartNodes_Success(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	client := dockerclient.NewMockAPIClient(t)
	containers := newKindContainers(testClusterName)

	client.EXPECT().
		ContainerList(ctx, container.ListOptions{All: true}).
		Return(containers, nil)
	client.EXPECT().
		ContainerStart(mock.Anything, testClusterName+"-control-plane", container.StartOptions{}).
		Return(nil)
	client.EXPECT().
		ContainerStart(mock.Anything, testClusterName+"-worker", container.StartOptions{}).
		Return(nil)

	prov := docker.NewProvider(client, docker.LabelSchemeKind)

	err := prov.StartNodes(ctx, testClusterName)

	require.NoError(t, err)
}

func TestProvider_StopNodes_Success(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	client := dockerclient.NewMockAPIClient(t)
	containers := newKindContainers(testClusterName)

	client.EXPECT().
		ContainerList(ctx, container.ListOptions{All: true}).
		Return(containers, nil)
	client.EXPECT().
		ContainerStop(mock.Anything, testClusterName+"-control-plane", container.StopOptions{}).
		Return(nil)
	client.EXPECT().
		ContainerStop(mock.Anything, testClusterName+"-worker", container.StopOptions{}).
		Return(nil)

	prov := docker.NewProvider(client, docker.LabelSchemeKind)

	err := prov.StopNodes(ctx, testClusterName)

	require.NoError(t, err)
}

func TestProvider_DeleteNodes_NilClient(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	prov := docker.NewProvider(nil, docker.LabelSchemeKind)

	err := prov.DeleteNodes(ctx, testClusterName)

	require.Error(t, err)
	require.ErrorIs(t, err, provider.ErrProviderUnavailable)
}

func TestProvider_DeleteNodes_Success(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	client := dockerclient.NewMockAPIClient(t)
	containers := newKindContainers(testClusterName)

	client.EXPECT().
		ContainerList(ctx, container.ListOptions{All: true}).
		Return(containers, nil)
	client.EXPECT().
		ContainerRemove(ctx, "cp1", container.RemoveOptions{Force: true, RemoveVolumes: true}).
		Return(nil)
	client.EXPECT().
		ContainerRemove(ctx, "w1", container.RemoveOptions{Force: true, RemoveVolumes: true}).
		Return(nil)

	prov := docker.NewProvider(client, docker.LabelSchemeKind)

	err := prov.DeleteNodes(ctx, testClusterName)

	require.NoError(t, err)
}

func TestLabelSchemeConstants(t *testing.T) {
	t.Parallel()

	// Test that label scheme constants are distinct
	schemes := []docker.LabelScheme{
		docker.LabelSchemeKind,
		docker.LabelSchemeK3d,
		docker.LabelSchemeTalos,
	}

	seen := make(map[docker.LabelScheme]bool)

	for _, scheme := range schemes {
		assert.NotEmpty(t, string(scheme))
		assert.False(t, seen[scheme], "duplicate label scheme: %s", scheme)

		seen[scheme] = true
	}
}

func TestLabelConstants(t *testing.T) {
	t.Parallel()

	// Talos labels
	assert.Equal(t, "talos.owned", docker.LabelTalosOwned)
	assert.Equal(t, "talos.cluster.name", docker.LabelTalosClusterName)
	assert.Equal(t, "talos.type", docker.LabelTalosType)

	// K3d labels
	assert.Equal(t, "k3d.cluster", docker.LabelK3dCluster)
	assert.Equal(t, "k3d.role", docker.LabelK3dRole)
}

func TestProvider_ListNodes_UnknownLabelScheme(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	client := dockerclient.NewMockAPIClient(t)
	prov := docker.NewProvider(client, docker.LabelScheme("unknown"))

	// For unknown scheme, listContainers will fail, not ContainerList
	// We need to test the error path

	nodes, err := prov.ListNodes(ctx, testClusterName)

	require.Error(t, err)
	require.ErrorIs(t, err, provider.ErrUnknownLabelScheme)
	assert.Nil(t, nodes)
}
