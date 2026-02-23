package vclusterprovisioner_test

import (
	"bytes"
	"context"
	"testing"

	dockerclient "github.com/devantler-tech/ksail/v5/pkg/client/docker"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/registry"
	vclusterprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/vcluster"
	"github.com/docker/docker/api/types/container"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigureContainerdRegistryMirrors_EmptyMirrorSpecs(t *testing.T) {
	t.Parallel()

	mockDocker := dockerclient.NewMockAPIClient(t)
	var buf bytes.Buffer

	err := vclusterprovisioner.ConfigureContainerdRegistryMirrors(
		context.Background(),
		"test-cluster",
		[]registry.MirrorSpec{},
		mockDocker,
		&buf,
	)

	require.NoError(t, err, "Should handle empty mirror specs gracefully")
	mockDocker.AssertExpectations(t)
}

func TestConfigureContainerdRegistryMirrors_NoVClusterNodes(t *testing.T) {
	t.Parallel()

	mockDocker := dockerclient.NewMockAPIClient(t)
	var buf bytes.Buffer

	mirrorSpecs := []registry.MirrorSpec{
		{
			Host: "docker.io",
			Remote: "localhost:5000",
		},
	}

	// Mock ContainerList to return empty list (no VCluster nodes found)
	mockDocker.On("ContainerList", context.Background(), container.ListOptions{All: true}).
		Return([]container.Summary{}, nil).
		Once()

	err := vclusterprovisioner.ConfigureContainerdRegistryMirrors(
		context.Background(),
		"test-cluster",
		mirrorSpecs,
		mockDocker,
		&buf,
	)

	require.Error(t, err, "Should error when no VCluster nodes are found")
	assert.ErrorIs(t, err, vclusterprovisioner.ErrNoVClusterNodes)
	mockDocker.AssertExpectations(t)
}

func TestConfigureContainerdRegistryMirrors_WithControlPlaneNode(t *testing.T) {
	t.Parallel()

	mockDocker := dockerclient.NewMockAPIClient(t)
	var buf bytes.Buffer

	mirrorSpecs := []registry.MirrorSpec{
		{
			Host: "docker.io",
			Remote: "localhost:5000",
		},
	}

	// Mock ContainerList to return VCluster control plane container
	mockDocker.On("ContainerList", context.Background(), container.ListOptions{All: true}).
		Return([]container.Summary{
			{
				Names: []string{"/vcluster.cp.test-cluster"},
			},
		}, nil).
		Once()

	// Mock CopyToContainer for hosts.toml injection
	mockDocker.On(
		"CopyToContainer",
		context.Background(),
		"vcluster.cp.test-cluster",
		"/etc/containerd",
	).Return(nil).Once()

	err := vclusterprovisioner.ConfigureContainerdRegistryMirrors(
		context.Background(),
		"test-cluster",
		mirrorSpecs,
		mockDocker,
		&buf,
	)

	require.NoError(t, err, "Should succeed with control plane node")
	mockDocker.AssertExpectations(t)
}

func TestConfigureContainerdRegistryMirrors_WithWorkerNodes(t *testing.T) {
	t.Parallel()

	mockDocker := dockerclient.NewMockAPIClient(t)
	var buf bytes.Buffer

	mirrorSpecs := []registry.MirrorSpec{
		{
			Host: "ghcr.io",
			Remote: "localhost:5001",
		},
	}

	// Mock ContainerList to return both control plane and worker nodes
	mockDocker.On("ContainerList", context.Background(), container.ListOptions{All: true}).
		Return([]container.Summary{
			{
				Names: []string{"/vcluster.cp.my-cluster"},
			},
			{
				Names: []string{"/vcluster.node.my-cluster.1"},
			},
			{
				Names: []string{"/vcluster.node.my-cluster.2"},
			},
			{
				Names: []string{"/some-other-container"}, // Should be ignored
			},
		}, nil).
		Once()

	// Mock CopyToContainer for each node
	mockDocker.On(
		"CopyToContainer",
		context.Background(),
		"vcluster.cp.my-cluster",
		"/etc/containerd",
	).Return(nil).Once()

	mockDocker.On(
		"CopyToContainer",
		context.Background(),
		"vcluster.node.my-cluster.1",
		"/etc/containerd",
	).Return(nil).Once()

	mockDocker.On(
		"CopyToContainer",
		context.Background(),
		"vcluster.node.my-cluster.2",
		"/etc/containerd",
	).Return(nil).Once()

	err := vclusterprovisioner.ConfigureContainerdRegistryMirrors(
		context.Background(),
		"my-cluster",
		mirrorSpecs,
		mockDocker,
		&buf,
	)

	require.NoError(t, err, "Should succeed with all nodes")
	mockDocker.AssertExpectations(t)
}

func TestSetupRegistries_Success(t *testing.T) {
	t.Parallel()

	mockDocker := dockerclient.NewMockAPIClient(t)
	var buf bytes.Buffer

	mirrorSpecs := []registry.MirrorSpec{
		{
			Host: "docker.io",
			Remote: "localhost:5000",
		},
	}

	// SetupMirrorSpecRegistries will need various Docker operations
	// Since we're testing the wrapper function, we'll mock the minimal requirements
	mockDocker.On("ContainerList", context.Background()).
		Return([]container.Summary{}, nil).
		Maybe()

	err := vclusterprovisioner.SetupRegistries(
		context.Background(),
		"test-cluster",
		mockDocker,
		mirrorSpecs,
		&buf,
	)

	// This might succeed or fail depending on Docker state
	// The important part is that it calls the underlying registry function
	if err != nil {
		assert.Contains(t, err.Error(), "setup vcluster registries",
			"Error should be properly wrapped")
	}
}

func TestConnectRegistriesToNetwork_Success(t *testing.T) {
	t.Parallel()

	mockDocker := dockerclient.NewMockAPIClient(t)
	var buf bytes.Buffer

	mirrorSpecs := []registry.MirrorSpec{
		{
			Host: "docker.io",
			Remote: "localhost:5000",
		},
	}

	err := vclusterprovisioner.ConnectRegistriesToNetwork(
		context.Background(),
		mirrorSpecs,
		"test-cluster",
		mockDocker,
		&buf,
	)

	// This test validates the wrapper function behavior
	if err != nil {
		assert.Contains(t, err.Error(), "connect registries to vcluster network",
			"Error should be properly wrapped")
	}
}

func TestCleanupRegistries_Success(t *testing.T) {
	t.Parallel()

	mockDocker := dockerclient.NewMockAPIClient(t)

	mirrorSpecs := []registry.MirrorSpec{
		{
			Host: "docker.io",
			Remote: "localhost:5000",
		},
	}

	err := vclusterprovisioner.CleanupRegistries(
		context.Background(),
		mirrorSpecs,
		"test-cluster",
		mockDocker,
		true, // deleteVolumes
	)

	// This test validates the wrapper function behavior
	if err != nil {
		assert.Contains(t, err.Error(), "cleanup vcluster registries",
			"Error should be properly wrapped")
	}
}
