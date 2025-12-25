package talosindockerprovisioner_test

import (
	"context"
	"io"
	"os"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/client/docker"
	talosconfigmanager "github.com/devantler-tech/ksail/v5/pkg/io/config-manager/talos"
	talosindockerprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/talosindocker"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/siderolabs/talos/pkg/provision"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// createTestTalosConfigs creates a minimal TalosConfigs for testing.
func createTestTalosConfigs(t *testing.T, clusterName string) *talosconfigmanager.Configs {
	t.Helper()

	// Create a temp directory with empty patch directories
	tempDir := t.TempDir()
	require.NoError(t, os.MkdirAll(tempDir+"/cluster", 0o750))
	require.NoError(t, os.MkdirAll(tempDir+"/control-planes", 0o750))
	require.NoError(t, os.MkdirAll(tempDir+"/workers", 0o750))

	manager := talosconfigmanager.NewConfigManager(tempDir, clusterName, "", "")
	configs, err := manager.LoadConfig(nil)
	require.NoError(t, err)

	return configs
}

// createTestTalosConfigsWithPatches creates TalosConfigs with a sample patch for testing.
func createTestTalosConfigsWithPatches(t *testing.T, clusterName string) *talosconfigmanager.Configs {
	t.Helper()

	tempDir := setupPatchDirectories(t)

	manager := talosconfigmanager.NewConfigManager(tempDir, clusterName, "", "")
	configs, err := manager.LoadConfig(nil)
	require.NoError(t, err)

	return configs
}

func TestNewTalosInDockerProvisioner(t *testing.T) {
	t.Parallel()

	configs := createTestTalosConfigs(t, "test-cluster")
	options := talosindockerprovisioner.NewOptions()
	provisioner := talosindockerprovisioner.NewTalosInDockerProvisioner(configs, options)

	require.NotNil(t, provisioner)
}

func TestNewTalosInDockerProvisioner_NilOptions(t *testing.T) {
	t.Parallel()

	configs := createTestTalosConfigs(t, "test-cluster")
	// Should create with default options when nil is passed
	provisioner := talosindockerprovisioner.NewTalosInDockerProvisioner(configs, nil)

	require.NotNil(t, provisioner)
}

func TestTalosInDockerProvisioner_Options(t *testing.T) {
	t.Parallel()

	configs := createTestTalosConfigs(t, "test-cluster")
	options := talosindockerprovisioner.NewOptions().
		WithKubeconfigPath("/tmp/kubeconfig")

	provisioner := talosindockerprovisioner.NewTalosInDockerProvisioner(configs, options)
	retrievedOptions := provisioner.Options()

	require.NotNil(t, retrievedOptions)
	assert.Equal(t, "/tmp/kubeconfig", retrievedOptions.KubeconfigPath)
}

func TestTalosInDockerProvisioner_TalosConfigs(t *testing.T) {
	t.Parallel()

	configs := createTestTalosConfigs(t, "test-cluster")
	provisioner := talosindockerprovisioner.NewTalosInDockerProvisioner(configs, nil)

	retrievedConfigs := provisioner.TalosConfigs()
	require.NotNil(t, retrievedConfigs)
	assert.Equal(t, "test-cluster", retrievedConfigs.Name)
}

func TestTalosInDockerProvisioner_Create_NoDockerClient(t *testing.T) {
	t.Parallel()

	configs := createTestTalosConfigs(t, "test-cluster")
	provisioner := talosindockerprovisioner.NewTalosInDockerProvisioner(configs, nil)

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

	configs := createTestTalosConfigs(t, "existing-cluster")
	provisioner := talosindockerprovisioner.NewTalosInDockerProvisioner(configs, nil).
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

	configs := createTestTalosConfigs(t, "test-cluster")
	provisioner := talosindockerprovisioner.NewTalosInDockerProvisioner(configs, nil).
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

	configs := createTestTalosConfigsWithPatches(t, "test-cluster-patches")
	provisioner := talosindockerprovisioner.NewTalosInDockerProvisioner(configs, nil).
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

	configs := createTestTalosConfigs(t, "test-cluster")
	provisioner := talosindockerprovisioner.NewTalosInDockerProvisioner(configs, nil)
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

	configs := createTestTalosConfigs(t, "test-cluster")
	provisioner := talosindockerprovisioner.NewTalosInDockerProvisioner(configs, nil).
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

	configs := createTestTalosConfigs(t, "test-cluster")
	provisioner := talosindockerprovisioner.NewTalosInDockerProvisioner(configs, nil).
		WithDockerClient(mockClient).
		WithLogWriter(io.Discard)

	ctx := context.Background()
	err := provisioner.Start(ctx, "test-cluster")

	require.NoError(t, err)
}

func TestTalosInDockerProvisioner_Stop_NoDockerClient(t *testing.T) {
	t.Parallel()

	configs := createTestTalosConfigs(t, "test-cluster")
	provisioner := talosindockerprovisioner.NewTalosInDockerProvisioner(configs, nil)
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

	configs := createTestTalosConfigs(t, "test-cluster")
	provisioner := talosindockerprovisioner.NewTalosInDockerProvisioner(configs, nil).
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

	configs := createTestTalosConfigs(t, "test-cluster")
	provisioner := talosindockerprovisioner.NewTalosInDockerProvisioner(configs, nil).
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

	configs := createTestTalosConfigs(t, "test-cluster")
	provisioner := talosindockerprovisioner.NewTalosInDockerProvisioner(configs, nil)
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

	configs := createTestTalosConfigs(t, "test-cluster")
	provisioner := talosindockerprovisioner.NewTalosInDockerProvisioner(configs, nil).
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

	configs := createTestTalosConfigs(t, "test-cluster")
	provisioner := talosindockerprovisioner.NewTalosInDockerProvisioner(configs, nil).
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

	configs := createTestTalosConfigs(t, "test-cluster")
	provisioner := talosindockerprovisioner.NewTalosInDockerProvisioner(configs, nil)
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

	configs := createTestTalosConfigs(t, "test-cluster")
	provisioner := talosindockerprovisioner.NewTalosInDockerProvisioner(configs, nil).
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

	configs := createTestTalosConfigs(t, "nonexistent-cluster")
	provisioner := talosindockerprovisioner.NewTalosInDockerProvisioner(configs, nil).
		WithDockerClient(mockClient)

	ctx := context.Background()
	exists, err := provisioner.Exists(ctx, "")

	require.NoError(t, err)
	assert.False(t, exists)
}

func TestTalosInDockerProvisioner_Delete_NoDockerClient(t *testing.T) {
	t.Parallel()

	configs := createTestTalosConfigs(t, "test-cluster")
	provisioner := talosindockerprovisioner.NewTalosInDockerProvisioner(configs, nil)

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

	configs := createTestTalosConfigs(t, "nonexistent-cluster")
	provisioner := talosindockerprovisioner.NewTalosInDockerProvisioner(configs, nil).
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

	configs := createTestTalosConfigs(t, "test-cluster-delete")
	provisioner := talosindockerprovisioner.NewTalosInDockerProvisioner(configs, nil).
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
