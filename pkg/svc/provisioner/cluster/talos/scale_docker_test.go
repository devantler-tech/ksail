package talosprovisioner_test

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/client/docker"
	configmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager"
	talosconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/talos"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/clusterupdate"
	talosprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/talos"
	"github.com/docker/docker/api/types/container"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

var errDockerDaemonUnavailable = errors.New("docker: connection refused")

// newScaleProvisioner builds a Provisioner wired with the given mock Docker client.
// TalosConfigs are generated fresh so every test has valid CP/worker configs.
func newScaleProvisioner(
	t *testing.T,
	mockClient *docker.MockAPIClient,
) *talosprovisioner.Provisioner {
	t.Helper()

	configs := createTestTalosConfigs(t, "scale-cluster")

	return talosprovisioner.NewProvisioner(configs, talosprovisioner.NewOptions()).
		WithDockerClient(mockClient).
		WithLogWriter(io.Discard)
}

// loadConfigs builds a TalosConfigs from a fresh temp directory.
func loadConfigs(t *testing.T) *talosconfigmanager.Configs {
	t.Helper()

	tempDir := t.TempDir()
	manager := talosconfigmanager.NewConfigManager(tempDir, "scale-cluster", "", "")
	configs, err := manager.Load(configmanager.LoadOptions{})
	require.NoError(t, err)

	return configs
}

func TestAddDockerNodes_ControlPlane_CreatesAllNodes(t *testing.T) {
	t.Parallel()

	mockClient := docker.NewMockAPIClient(t)

	// listDockerNodesByRole: no existing CP nodes
	mockClient.On("ContainerList", mock.Anything, mock.Anything).
		Return([]container.Summary{}, nil).Once()

	// Three parallel ContainerCreate calls (order not guaranteed)
	mockClient.On(
		"ContainerCreate",
		mock.Anything, mock.Anything, mock.Anything,
		mock.Anything, mock.Anything,
		mock.AnythingOfType("string"),
	).
		Return(container.CreateResponse{ID: "container-id"}, nil).
		Times(3)

	// Three ContainerStart calls
	mockClient.On("ContainerStart", mock.Anything, mock.Anything, mock.Anything).
		Return(nil).Times(3)

	_ = loadConfigs(t) // validates configs can be loaded

	provisioner := newScaleProvisioner(t, mockClient)
	result := clusterupdate.NewEmptyUpdateResult()

	err := provisioner.AddDockerNodesForTest(
		context.Background(),
		"scale-cluster",
		talosprovisioner.RoleControlPlane,
		3,
		result,
	)

	require.NoError(t, err)
	assert.Len(t, result.AppliedChanges, 3, "expected 3 applied changes")
	assert.Empty(t, result.FailedChanges, "expected no failed changes")
}

func TestAddDockerNodes_Workers_CreatesAllNodes(t *testing.T) {
	t.Parallel()

	mockClient := docker.NewMockAPIClient(t)

	// 1 call for listDockerNodesByRole (existing workers) + 1 call for countDockerRole (CP count)
	mockClient.On("ContainerList", mock.Anything, mock.Anything).
		Return([]container.Summary{}, nil).Times(2)

	mockClient.On(
		"ContainerCreate",
		mock.Anything, mock.Anything, mock.Anything,
		mock.Anything, mock.Anything,
		mock.AnythingOfType("string"),
	).
		Return(container.CreateResponse{ID: "container-id"}, nil).
		Times(2)

	mockClient.On("ContainerStart", mock.Anything, mock.Anything, mock.Anything).
		Return(nil).Times(2)

	provisioner := newScaleProvisioner(t, mockClient)
	result := clusterupdate.NewEmptyUpdateResult()

	err := provisioner.AddDockerNodesForTest(
		context.Background(),
		"scale-cluster",
		talosprovisioner.RoleWorker,
		2,
		result,
	)

	require.NoError(t, err)
	assert.Len(t, result.AppliedChanges, 2, "expected 2 applied changes")
	assert.Empty(t, result.FailedChanges, "expected no failed changes")
}

func TestAddDockerNodes_ContainerCreateFails_ReturnsError(t *testing.T) {
	t.Parallel()

	mockClient := docker.NewMockAPIClient(t)

	// listDockerNodesByRole: no existing nodes
	mockClient.On("ContainerList", mock.Anything, mock.Anything).
		Return([]container.Summary{}, nil).Once()

	// All container creations fail
	mockClient.On(
		"ContainerCreate",
		mock.Anything, mock.Anything, mock.Anything,
		mock.Anything, mock.Anything,
		mock.AnythingOfType("string"),
	).
		Return(container.CreateResponse{}, errDockerDaemonUnavailable).
		Times(2)

	provisioner := newScaleProvisioner(t, mockClient)
	result := clusterupdate.NewEmptyUpdateResult()

	err := provisioner.AddDockerNodesForTest(
		context.Background(),
		"scale-cluster",
		talosprovisioner.RoleControlPlane,
		2,
		result,
	)

	require.Error(t, err)
	require.ErrorContains(t, err, "failed to create")
	assert.Len(t, result.FailedChanges, 2, "both failed changes should be recorded")
	assert.Empty(t, result.AppliedChanges, "no nodes should have been applied")
}

func TestAddDockerNodes_ZeroCount_NoOp(t *testing.T) {
	t.Parallel()

	mockClient := docker.NewMockAPIClient(t)

	// listDockerNodesByRole: no existing nodes
	mockClient.On("ContainerList", mock.Anything, mock.Anything).
		Return([]container.Summary{}, nil).Once()

	provisioner := newScaleProvisioner(t, mockClient)
	result := clusterupdate.NewEmptyUpdateResult()

	err := provisioner.AddDockerNodesForTest(
		context.Background(),
		"scale-cluster",
		talosprovisioner.RoleControlPlane,
		0,
		result,
	)

	require.NoError(t, err)
	assert.Empty(t, result.AppliedChanges)
	assert.Empty(t, result.FailedChanges)
}

func TestAddDockerNodes_ListExistingFails_ReturnsError(t *testing.T) {
	t.Parallel()

	mockClient := docker.NewMockAPIClient(t)

	// listDockerNodesByRole fails
	mockClient.On("ContainerList", mock.Anything, mock.Anything).
		Return([]container.Summary{}, errDockerDaemonUnavailable).Once()

	provisioner := newScaleProvisioner(t, mockClient)
	result := clusterupdate.NewEmptyUpdateResult()

	err := provisioner.AddDockerNodesForTest(
		context.Background(),
		"scale-cluster",
		talosprovisioner.RoleControlPlane,
		1,
		result,
	)

	require.Error(t, err)
	require.ErrorContains(t, err, "failed to list")
	assert.Empty(t, result.AppliedChanges)
}

func TestRemoveDockerNodes_Workers_RemovesAll(t *testing.T) {
	t.Parallel()

	mockClient := docker.NewMockAPIClient(t)

	// listDockerNodesByRole: two existing worker nodes
	mockClient.On("ContainerList", mock.Anything, mock.Anything).
		Return([]container.Summary{
			{ID: "worker-1", Names: []string{"/scale-cluster-worker-1"}},
			{ID: "worker-2", Names: []string{"/scale-cluster-worker-2"}},
		}, nil).Once()

	// ContainerStop is best-effort (result ignored)
	mockClient.On("ContainerStop", mock.Anything, mock.Anything, mock.Anything).
		Return(nil).Times(2)

	// ContainerRemove succeeds for both workers
	mockClient.On("ContainerRemove", mock.Anything, mock.Anything, mock.Anything).
		Return(nil).Times(2)

	provisioner := newScaleProvisioner(t, mockClient)
	result := clusterupdate.NewEmptyUpdateResult()

	err := provisioner.RemoveDockerNodesForTest(
		context.Background(),
		"scale-cluster",
		talosprovisioner.RoleWorker,
		2,
		result,
	)

	require.NoError(t, err)
	assert.Len(t, result.AppliedChanges, 2, "expected 2 applied changes")
	assert.Empty(t, result.FailedChanges, "expected no failed changes")
}

func TestRemoveDockerNodes_Workers_ContainerRemoveFails_ReturnsError(t *testing.T) {
	t.Parallel()

	mockClient := docker.NewMockAPIClient(t)

	// listDockerNodesByRole: one existing worker node
	mockClient.On("ContainerList", mock.Anything, mock.Anything).
		Return([]container.Summary{
			{ID: "worker-1", Names: []string{"/scale-cluster-worker-1"}},
		}, nil).Once()

	// ContainerStop is best-effort
	mockClient.On("ContainerStop", mock.Anything, mock.Anything, mock.Anything).
		Return(nil).Once()

	// ContainerRemove fails
	mockClient.On("ContainerRemove", mock.Anything, mock.Anything, mock.Anything).
		Return(errDockerDaemonUnavailable).Once()

	provisioner := newScaleProvisioner(t, mockClient)
	result := clusterupdate.NewEmptyUpdateResult()

	err := provisioner.RemoveDockerNodesForTest(
		context.Background(),
		"scale-cluster",
		talosprovisioner.RoleWorker,
		1,
		result,
	)

	require.Error(t, err)
	require.ErrorContains(t, err, "failed to remove")
	assert.Len(t, result.FailedChanges, 1, "expected 1 failed change")
	assert.Empty(t, result.AppliedChanges, "no nodes should have been applied")
}

func TestRemoveDockerNodes_Workers_ZeroCount_NoOp(t *testing.T) {
	t.Parallel()

	mockClient := docker.NewMockAPIClient(t)

	// listDockerNodesByRole: one existing worker node
	mockClient.On("ContainerList", mock.Anything, mock.Anything).
		Return([]container.Summary{
			{ID: "worker-1", Names: []string{"/scale-cluster-worker-1"}},
		}, nil).Once()

	provisioner := newScaleProvisioner(t, mockClient)
	result := clusterupdate.NewEmptyUpdateResult()

	err := provisioner.RemoveDockerNodesForTest(
		context.Background(),
		"scale-cluster",
		talosprovisioner.RoleWorker,
		0,
		result,
	)

	require.NoError(t, err)
	assert.Empty(t, result.AppliedChanges)
	assert.Empty(t, result.FailedChanges)
}

func TestRemoveDockerNodes_Workers_ListFails_ReturnsError(t *testing.T) {
	t.Parallel()

	mockClient := docker.NewMockAPIClient(t)

	// listDockerNodesByRole fails
	mockClient.On("ContainerList", mock.Anything, mock.Anything).
		Return([]container.Summary{}, errDockerDaemonUnavailable).Once()

	provisioner := newScaleProvisioner(t, mockClient)
	result := clusterupdate.NewEmptyUpdateResult()

	err := provisioner.RemoveDockerNodesForTest(
		context.Background(),
		"scale-cluster",
		talosprovisioner.RoleWorker,
		1,
		result,
	)

	require.Error(t, err)
	require.ErrorContains(t, err, "listing existing")
	assert.Empty(t, result.AppliedChanges)
	assert.Empty(t, result.FailedChanges)
}
