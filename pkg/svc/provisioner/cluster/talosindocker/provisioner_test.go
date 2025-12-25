package talosindockerprovisioner_test

import (
	"context"
	"io"
	"os"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/client/docker"
	talosindockerprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/talosindocker"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/siderolabs/talos/pkg/provision"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestNewTalosInDockerProvisioner(t *testing.T) {
	t.Parallel()

	config := talosindockerprovisioner.NewTalosInDockerConfig()
	provisioner := talosindockerprovisioner.NewTalosInDockerProvisioner(config)

	require.NotNil(t, provisioner)
}

func TestNewTalosInDockerProvisioner_NilConfig(t *testing.T) {
	t.Parallel()

	// Should create with default config when nil is passed
	provisioner := talosindockerprovisioner.NewTalosInDockerProvisioner(nil)

	require.NotNil(t, provisioner)
}

func TestTalosInDockerProvisioner_Config(t *testing.T) {
	t.Parallel()

	config := talosindockerprovisioner.NewTalosInDockerConfig().
		WithClusterName("test-cluster")

	provisioner := talosindockerprovisioner.NewTalosInDockerProvisioner(config)
	retrievedConfig := provisioner.Config()

	require.NotNil(t, retrievedConfig)
	assert.Equal(t, "test-cluster", retrievedConfig.ClusterName)
}

func TestTalosInDockerProvisioner_Create_NoDockerClient(t *testing.T) {
	t.Parallel()

	config := talosindockerprovisioner.NewTalosInDockerConfig()
	provisioner := talosindockerprovisioner.NewTalosInDockerProvisioner(config)

	ctx := context.Background()
	err := provisioner.Create(ctx, "")

	// Create requires Docker client to check if cluster exists
	require.Error(t, err)
	require.ErrorIs(t, err, talosindockerprovisioner.ErrDockerNotAvailable)
}

func TestTalosInDockerProvisioner_Create_ClusterAlreadyExists(t *testing.T) {
	t.Parallel()

	mockClient := docker.NewMockAPIClient(t)
	mockClient.EXPECT().
		Ping(mock.Anything).
		Return(types.Ping{}, nil)
	mockClient.EXPECT().
		ContainerList(mock.Anything, mock.Anything).
		Return([]container.Summary{
			{
				Labels: map[string]string{
					talosindockerprovisioner.LabelTalosOwned:       "true",
					talosindockerprovisioner.LabelTalosClusterName: "existing-cluster",
				},
			},
		}, nil)

	config := talosindockerprovisioner.NewTalosInDockerConfig()
	provisioner := talosindockerprovisioner.NewTalosInDockerProvisioner(config).
		WithDockerClient(mockClient)

	ctx := context.Background()
	err := provisioner.Create(ctx, "existing-cluster")

	require.Error(t, err)
	require.ErrorIs(t, err, talosindockerprovisioner.ErrClusterAlreadyExists)
}

func TestTalosInDockerProvisioner_Create_Success(t *testing.T) {
	t.Parallel()

	// Mock Docker client - no existing clusters
	mockClient := docker.NewMockAPIClient(t)
	mockClient.EXPECT().
		Ping(mock.Anything).
		Return(types.Ping{}, nil)
	mockClient.EXPECT().
		ContainerList(mock.Anything, mock.Anything).
		Return([]container.Summary{}, nil)

	// Mock Cluster to return from Create
	mockCluster := NewMockCluster()
	mockCluster.On("Info").Return(provision.ClusterInfo{
		ClusterName: "test-cluster",
		Nodes:       []provision.NodeInfo{{Name: "node-1"}},
	})

	// Mock Provisioner
	mockProvisioner := NewMockProvisioner()
	mockProvisioner.On("Create", mock.Anything, mock.Anything, mock.Anything).
		Return(mockCluster, nil)
	mockProvisioner.On("Close").Return(nil)

	config := talosindockerprovisioner.NewTalosInDockerConfig()
	provisioner := talosindockerprovisioner.NewTalosInDockerProvisioner(config).
		WithDockerClient(mockClient).
		WithProvisionerFactory(func(_ context.Context) (provision.Provisioner, error) {
			return mockProvisioner, nil
		}).
		WithLogWriter(io.Discard)

	ctx := context.Background()
	err := provisioner.Create(ctx, "test-cluster")

	require.NoError(t, err)
	mockProvisioner.AssertExpectations(t)
	mockCluster.AssertExpectations(t)
}

func TestTalosInDockerProvisioner_Create_WithPatches(t *testing.T) {
	t.Parallel()

	// Setup patch directories and write sample patch
	tempDir := setupPatchDirectories(t)

	// Mock Docker client - no existing clusters
	mockClient := docker.NewMockAPIClient(t)
	mockClient.EXPECT().
		Ping(mock.Anything).
		Return(types.Ping{}, nil)
	mockClient.EXPECT().
		ContainerList(mock.Anything, mock.Anything).
		Return([]container.Summary{}, nil)

	// Mock Cluster to return from Create
	mockCluster := NewMockCluster()
	mockCluster.On("Info").Return(provision.ClusterInfo{
		ClusterName: "test-cluster-patches",
		Nodes:       []provision.NodeInfo{{Name: "node-1"}},
	})

	// Mock Provisioner - capture the ClusterRequest to verify configs
	mockProvisioner := NewMockProvisioner()
	mockProvisioner.On("Create", mock.Anything, mock.MatchedBy(
		verifyNodesHaveConfigs,
	), mock.Anything).Return(mockCluster, nil)
	mockProvisioner.On("Close").Return(nil)

	config := talosindockerprovisioner.NewTalosInDockerConfig().
		WithClusterName("test-cluster-patches").
		WithPatchesDir(tempDir)
	provisioner := talosindockerprovisioner.NewTalosInDockerProvisioner(config).
		WithDockerClient(mockClient).
		WithProvisionerFactory(func(_ context.Context) (provision.Provisioner, error) {
			return mockProvisioner, nil
		}).
		WithLogWriter(io.Discard)

	ctx := context.Background()
	err := provisioner.Create(ctx, "test-cluster-patches")

	require.NoError(t, err)
	mockProvisioner.AssertExpectations(t)
	mockCluster.AssertExpectations(t)
}

// setupPatchDirectories creates temp patch directories and a sample patch file.
func setupPatchDirectories(t *testing.T) string {
	t.Helper()

	tempDir := t.TempDir()
	clusterPatchesDir := tempDir + "/cluster"

	require.NoError(t, os.MkdirAll(clusterPatchesDir, 0o750))
	require.NoError(t, os.MkdirAll(tempDir+"/control-planes", 0o750))
	require.NoError(t, os.MkdirAll(tempDir+"/workers", 0o750))

	// Write a sample cluster patch
	clusterPatchContent := "machine:\n  network:\n    hostname: test-hostname\n"
	//nolint:gosec // G306: test file, permissions are fine
	require.NoError(t, os.WriteFile(
		clusterPatchesDir+"/hostname.yaml",
		[]byte(clusterPatchContent),
		0o644,
	))

	return tempDir
}

// verifyNodesHaveConfigs checks that all nodes in the request have configs assigned.
func verifyNodesHaveConfigs(req provision.ClusterRequest) bool {
	if len(req.Nodes) == 0 {
		return false
	}

	for _, node := range req.Nodes {
		if node.Config == nil {
			return false
		}
	}

	return true
}

func TestTalosInDockerProvisioner_Start_NoDockerClient(t *testing.T) {
	t.Parallel()

	config := talosindockerprovisioner.NewTalosInDockerConfig()
	provisioner := talosindockerprovisioner.NewTalosInDockerProvisioner(config)
	// No Docker client set

	ctx := context.Background()
	err := provisioner.Start(ctx, "")

	require.Error(t, err)
	assert.ErrorIs(t, err, talosindockerprovisioner.ErrDockerNotAvailable)
}

func TestTalosInDockerProvisioner_Start_ClusterNotFound(t *testing.T) {
	t.Parallel()

	mockClient := docker.NewMockAPIClient(t)
	mockClient.EXPECT().Ping(mock.Anything).Return(types.Ping{}, nil)
	mockClient.EXPECT().
		ContainerList(mock.Anything, mock.Anything).
		Return([]container.Summary{}, nil)

	config := talosindockerprovisioner.NewTalosInDockerConfig()
	provisioner := talosindockerprovisioner.NewTalosInDockerProvisioner(config).
		WithDockerClient(mockClient)

	ctx := context.Background()
	err := provisioner.Start(ctx, "nonexistent")

	require.Error(t, err)
	assert.ErrorIs(t, err, talosindockerprovisioner.ErrClusterNotFound)
}

func TestTalosInDockerProvisioner_Start_Success(t *testing.T) {
	t.Parallel()

	mockClient := docker.NewMockAPIClient(t)
	setupContainerOperationMock(mockClient, "container-1", "test-cluster", true)

	config := talosindockerprovisioner.NewTalosInDockerConfig()
	provisioner := talosindockerprovisioner.NewTalosInDockerProvisioner(config).
		WithDockerClient(mockClient).
		WithLogWriter(io.Discard)

	ctx := context.Background()
	err := provisioner.Start(ctx, "test-cluster")

	require.NoError(t, err)
}

func TestTalosInDockerProvisioner_Stop_NoDockerClient(t *testing.T) {
	t.Parallel()

	config := talosindockerprovisioner.NewTalosInDockerConfig()
	provisioner := talosindockerprovisioner.NewTalosInDockerProvisioner(config)
	// No Docker client set

	ctx := context.Background()
	err := provisioner.Stop(ctx, "")

	require.Error(t, err)
	assert.ErrorIs(t, err, talosindockerprovisioner.ErrDockerNotAvailable)
}

func TestTalosInDockerProvisioner_Stop_ClusterNotFound(t *testing.T) {
	t.Parallel()

	mockClient := docker.NewMockAPIClient(t)
	mockClient.EXPECT().Ping(mock.Anything).Return(types.Ping{}, nil)
	mockClient.EXPECT().
		ContainerList(mock.Anything, mock.Anything).
		Return([]container.Summary{}, nil)

	config := talosindockerprovisioner.NewTalosInDockerConfig()
	provisioner := talosindockerprovisioner.NewTalosInDockerProvisioner(config).
		WithDockerClient(mockClient)

	ctx := context.Background()
	err := provisioner.Stop(ctx, "nonexistent")

	require.Error(t, err)
	assert.ErrorIs(t, err, talosindockerprovisioner.ErrClusterNotFound)
}

func TestTalosInDockerProvisioner_Stop_Success(t *testing.T) {
	t.Parallel()

	mockClient := docker.NewMockAPIClient(t)
	setupContainerOperationMock(mockClient, "container-1", "test-cluster", false)

	config := talosindockerprovisioner.NewTalosInDockerConfig()
	provisioner := talosindockerprovisioner.NewTalosInDockerProvisioner(config).
		WithDockerClient(mockClient).
		WithLogWriter(io.Discard)

	ctx := context.Background()
	err := provisioner.Stop(ctx, "test-cluster")

	require.NoError(t, err)
}

// setupContainerOperationMock configures mock expectations for Start/Stop operations.
// isStart=true sets up ContainerStart mock, isStart=false sets up ContainerStop mock.
func setupContainerOperationMock(
	mockClient *docker.MockAPIClient,
	containerID, clusterName string,
	isStart bool,
) {
	containerSummary := container.Summary{
		ID: containerID,
		Labels: map[string]string{
			talosindockerprovisioner.LabelTalosOwned:       "true",
			talosindockerprovisioner.LabelTalosClusterName: clusterName,
		},
		Names: []string{"/" + clusterName + "-control-plane-1"},
	}

	mockClient.EXPECT().Ping(mock.Anything).Return(types.Ping{}, nil)
	// Exists check
	mockClient.EXPECT().
		ContainerList(mock.Anything, mock.Anything).
		Return([]container.Summary{containerSummary}, nil).Once()
	// List containers for operation
	mockClient.EXPECT().
		ContainerList(mock.Anything, mock.Anything).
		Return([]container.Summary{containerSummary}, nil).Once()

	if isStart {
		mockClient.EXPECT().
			ContainerStart(mock.Anything, containerID, mock.Anything).
			Return(nil)
	} else {
		mockClient.EXPECT().
			ContainerStop(mock.Anything, containerID, mock.Anything).
			Return(nil)
	}
}

func TestTalosInDockerProvisioner_List_NoDockerClient(t *testing.T) {
	t.Parallel()

	config := talosindockerprovisioner.NewTalosInDockerConfig()
	provisioner := talosindockerprovisioner.NewTalosInDockerProvisioner(config)
	// No Docker client set

	ctx := context.Background()
	clusters, err := provisioner.List(ctx)

	require.Error(t, err)
	require.ErrorIs(t, err, talosindockerprovisioner.ErrDockerNotAvailable)
	assert.Nil(t, clusters)
}

func TestTalosInDockerProvisioner_List_EmptyResult(t *testing.T) {
	t.Parallel()

	mockClient := docker.NewMockAPIClient(t)
	mockClient.EXPECT().
		ContainerList(mock.Anything, mock.Anything).
		Return([]container.Summary{}, nil)

	config := talosindockerprovisioner.NewTalosInDockerConfig()
	provisioner := talosindockerprovisioner.NewTalosInDockerProvisioner(config).
		WithDockerClient(mockClient)

	ctx := context.Background()
	clusters, err := provisioner.List(ctx)

	require.NoError(t, err)
	assert.Empty(t, clusters)
}

func TestTalosInDockerProvisioner_List_WithClusters(t *testing.T) {
	t.Parallel()

	mockClient := docker.NewMockAPIClient(t)
	mockClient.EXPECT().
		ContainerList(mock.Anything, mock.Anything).
		Return([]container.Summary{
			{
				Labels: map[string]string{
					talosindockerprovisioner.LabelTalosOwned:       "true",
					talosindockerprovisioner.LabelTalosClusterName: "cluster-1",
				},
			},
			{
				Labels: map[string]string{
					talosindockerprovisioner.LabelTalosOwned:       "true",
					talosindockerprovisioner.LabelTalosClusterName: "cluster-1",
				},
			},
			{
				Labels: map[string]string{
					talosindockerprovisioner.LabelTalosOwned:       "true",
					talosindockerprovisioner.LabelTalosClusterName: "cluster-2",
				},
			},
		}, nil)

	config := talosindockerprovisioner.NewTalosInDockerConfig()
	provisioner := talosindockerprovisioner.NewTalosInDockerProvisioner(config).
		WithDockerClient(mockClient)

	ctx := context.Background()
	clusters, err := provisioner.List(ctx)

	require.NoError(t, err)
	assert.Len(t, clusters, 2)
	assert.Contains(t, clusters, "cluster-1")
	assert.Contains(t, clusters, "cluster-2")
}

func TestTalosInDockerProvisioner_Exists_NoDockerClient(t *testing.T) {
	t.Parallel()

	config := talosindockerprovisioner.NewTalosInDockerConfig()
	provisioner := talosindockerprovisioner.NewTalosInDockerProvisioner(config)
	// No Docker client set

	ctx := context.Background()
	exists, err := provisioner.Exists(ctx, "")

	require.Error(t, err)
	require.ErrorIs(t, err, talosindockerprovisioner.ErrDockerNotAvailable)
	assert.False(t, exists)
}

func TestTalosInDockerProvisioner_Exists_ClusterExists(t *testing.T) {
	t.Parallel()

	mockClient := docker.NewMockAPIClient(t)
	mockClient.EXPECT().
		ContainerList(mock.Anything, mock.Anything).
		Return([]container.Summary{
			{
				Labels: map[string]string{
					talosindockerprovisioner.LabelTalosOwned:       "true",
					talosindockerprovisioner.LabelTalosClusterName: "test-cluster",
				},
			},
		}, nil)

	config := talosindockerprovisioner.NewTalosInDockerConfig().
		WithClusterName("test-cluster")
	provisioner := talosindockerprovisioner.NewTalosInDockerProvisioner(config).
		WithDockerClient(mockClient)

	ctx := context.Background()
	exists, err := provisioner.Exists(ctx, "")

	require.NoError(t, err)
	assert.True(t, exists)
}

func TestTalosInDockerProvisioner_Exists_ClusterNotFound(t *testing.T) {
	t.Parallel()

	mockClient := docker.NewMockAPIClient(t)
	mockClient.EXPECT().
		ContainerList(mock.Anything, mock.Anything).
		Return([]container.Summary{}, nil)

	config := talosindockerprovisioner.NewTalosInDockerConfig().
		WithClusterName("nonexistent-cluster")
	provisioner := talosindockerprovisioner.NewTalosInDockerProvisioner(config).
		WithDockerClient(mockClient)

	ctx := context.Background()
	exists, err := provisioner.Exists(ctx, "")

	require.NoError(t, err)
	assert.False(t, exists)
}

func TestTalosInDockerProvisioner_Create_WithMirrorRegistries(t *testing.T) {
	t.Parallel()

	// Mock Docker client - no existing clusters
	mockClient := docker.NewMockAPIClient(t)
	mockClient.EXPECT().
		Ping(mock.Anything).
		Return(types.Ping{}, nil)
	mockClient.EXPECT().
		ContainerList(mock.Anything, mock.Anything).
		Return([]container.Summary{}, nil)

	// Mock Cluster to return from Create
	mockCluster := NewMockCluster()
	mockCluster.On("Info").Return(provision.ClusterInfo{
		ClusterName: "test-cluster-mirrors",
		Nodes:       []provision.NodeInfo{{Name: "node-1"}},
	})

	// Mock Provisioner
	mockProvisioner := NewMockProvisioner()
	mockProvisioner.On("Create", mock.Anything, mock.MatchedBy(
		verifyNodesHaveConfigs,
	), mock.Anything).Return(mockCluster, nil)
	mockProvisioner.On("Close").Return(nil)

	// Configure with mirror registries
	config := talosindockerprovisioner.NewTalosInDockerConfig().
		WithClusterName("test-cluster-mirrors").
		WithMirrorRegistries([]string{"docker.io=https://registry-1.docker.io"})

	provisioner := talosindockerprovisioner.NewTalosInDockerProvisioner(config).
		WithDockerClient(mockClient).
		WithProvisionerFactory(func(_ context.Context) (provision.Provisioner, error) {
			return mockProvisioner, nil
		}).
		WithLogWriter(io.Discard)

	ctx := context.Background()
	err := provisioner.Create(ctx, "test-cluster-mirrors")

	require.NoError(t, err)
	mockProvisioner.AssertExpectations(t)
	mockCluster.AssertExpectations(t)
}

func TestTalosInDockerProvisioner_Delete_NoDockerClient(t *testing.T) {
	t.Parallel()

	config := talosindockerprovisioner.NewTalosInDockerConfig().
		WithClusterName("test-cluster")
	provisioner := talosindockerprovisioner.NewTalosInDockerProvisioner(config)

	ctx := context.Background()
	err := provisioner.Delete(ctx, "")

	require.Error(t, err)
	require.ErrorIs(t, err, talosindockerprovisioner.ErrDockerNotAvailable)
}

func TestTalosInDockerProvisioner_Delete_ClusterNotFound(t *testing.T) {
	t.Parallel()

	// Mock Docker client - no existing clusters
	mockClient := docker.NewMockAPIClient(t)
	mockClient.EXPECT().
		Ping(mock.Anything).
		Return(types.Ping{}, nil)
	mockClient.EXPECT().
		ContainerList(mock.Anything, mock.Anything).
		Return([]container.Summary{}, nil)

	config := talosindockerprovisioner.NewTalosInDockerConfig().
		WithClusterName("nonexistent-cluster")
	provisioner := talosindockerprovisioner.NewTalosInDockerProvisioner(config).
		WithDockerClient(mockClient)

	ctx := context.Background()
	err := provisioner.Delete(ctx, "")

	require.Error(t, err)
	require.ErrorIs(t, err, talosindockerprovisioner.ErrClusterNotFound)
}

func TestTalosInDockerProvisioner_Delete_Success(t *testing.T) {
	t.Parallel()

	// Mock Docker client - cluster exists
	mockClient := docker.NewMockAPIClient(t)
	mockClient.EXPECT().
		Ping(mock.Anything).
		Return(types.Ping{}, nil)
	mockClient.EXPECT().
		ContainerList(mock.Anything, mock.Anything).
		Return([]container.Summary{
			{
				Labels: map[string]string{
					talosindockerprovisioner.LabelTalosOwned:       "true",
					talosindockerprovisioner.LabelTalosClusterName: "test-cluster-delete",
				},
			},
		}, nil)

	// Mock Cluster to return from Reflect
	mockCluster := NewMockCluster()

	// Mock Provisioner
	mockProvisioner := NewMockProvisioner()
	mockProvisioner.On("Reflect", mock.Anything, "test-cluster-delete", mock.Anything).
		Return(mockCluster, nil)
	mockProvisioner.On("Destroy", mock.Anything, mockCluster, mock.Anything).
		Return(nil)
	mockProvisioner.On("Close").Return(nil)

	config := talosindockerprovisioner.NewTalosInDockerConfig().
		WithClusterName("test-cluster-delete")
	provisioner := talosindockerprovisioner.NewTalosInDockerProvisioner(config).
		WithDockerClient(mockClient).
		WithProvisionerFactory(func(_ context.Context) (provision.Provisioner, error) {
			return mockProvisioner, nil
		}).
		WithLogWriter(io.Discard)

	ctx := context.Background()
	err := provisioner.Delete(ctx, "test-cluster-delete")

	require.NoError(t, err)
	mockProvisioner.AssertExpectations(t)
	mockCluster.AssertExpectations(t)
}
