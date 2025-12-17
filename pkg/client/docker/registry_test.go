package docker_test

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	docker "github.com/devantler-tech/ksail-go/pkg/client/docker"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/api/types/volume"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

var (
	errNotFound = errors.New("not found")

	errImagePullFailed       = errors.New("image pull failed")
	errVolumeCreateFailed    = errors.New("volume create failed")
	errContainerCreateFailed = errors.New("container create failed")
	errContainerStartFailed  = errors.New("container start failed")
	errStopFailed            = errors.New("stop failed")
	errRemoveFailed          = errors.New("remove failed")
	errVolumeRemoveFailed    = errors.New("volume remove failed")
	errListFailed            = errors.New("list failed")
	errReadFailed            = errors.New("read failed")
)

// setupTestRegistryManager creates a test setup with mock client, manager, and context.
func setupTestRegistryManager(
	t *testing.T,
) (*docker.MockAPIClient, *docker.RegistryManager, context.Context) {
	t.Helper()

	mockClient := docker.NewMockAPIClient(t)
	manager, err := docker.NewRegistryManager(mockClient)
	require.NoError(t, err)

	ctx := context.Background()

	return mockClient, manager, ctx
}

// mockRegistryNotExists sets up mocks for when a registry doesn't exist.
func mockRegistryNotExists(ctx context.Context, mockClient *docker.MockAPIClient) {
	mockClient.EXPECT().
		ContainerList(ctx, mock.Anything).
		Return([]container.Summary{}, nil).
		Once()
}

// newTestRegistryConfig creates a standard test registry configuration.
func newTestRegistryConfig() docker.RegistryConfig {
	return docker.RegistryConfig{
		Name:        "docker.io",
		Port:        5000,
		UpstreamURL: "https://registry-1.docker.io",
		ClusterName: "test-cluster",
	}
}

func setupRegistryCreationTest(
	t *testing.T,
) (*docker.MockAPIClient, *docker.RegistryManager, context.Context, docker.RegistryConfig) {
	t.Helper()

	mockClient, manager, ctx := setupTestRegistryManager(t)
	config := newTestRegistryConfig()

	mockRegistryNotExists(ctx, mockClient)

	return mockClient, manager, ctx, config
}

func setupRegistryWithState(
	t *testing.T,
	registryID string,
	registryName string,
	state string,
) (*docker.MockAPIClient, *docker.RegistryManager, context.Context) {
	t.Helper()

	mockClient, manager, ctx := setupTestRegistryManager(t)
	mockContainerListOnce(ctx, mockClient, []container.Summary{
		mockRegistryContainer(registryID, registryName, "", state),
	})

	return mockClient, manager, ctx
}

func mockVolumeInspectMissing(
	ctx context.Context,
	mockClient *docker.MockAPIClient,
	registryName string,
) string {
	volumeName := docker.NormalizeVolumeName(registryName)

	mockClient.EXPECT().
		VolumeInspect(ctx, volumeName).
		Return(volume.Volume{}, errNotFound).
		Once()

	return volumeName
}

func expectVolumeCreate(
	ctx context.Context,
	mockClient *docker.MockAPIClient,
	volumeName string,
	returnErr error,
) {
	mockClient.EXPECT().
		VolumeCreate(ctx, mock.MatchedBy(func(opts volume.CreateOptions) bool {
			return opts.Name == volumeName
		})).
		Return(volume.Volume{}, returnErr).
		Once()
}

// mockImagePullSequence sets up the complete image pull mock sequence.
func mockImagePullSequence(ctx context.Context, mockClient *docker.MockAPIClient) {
	// Mock image inspect (image doesn't exist)
	mockClient.EXPECT().
		ImageInspect(ctx, docker.RegistryImageName).
		Return(image.InspectResponse{}, errNotFound).
		Once()

	// Mock image pull
	mockClient.EXPECT().
		ImagePull(ctx, docker.RegistryImageName, mock.Anything).
		Return(io.NopCloser(strings.NewReader("")), nil).
		Once()
}

// mockVolumeCreateSequence sets up the complete volume creation mock sequence.
//

func mockVolumeCreateSequence(
	ctx context.Context,
	mockClient *docker.MockAPIClient,
	registryName string,
) {
	volumeName := mockVolumeInspectMissing(ctx, mockClient, registryName)
	expectVolumeCreate(ctx, mockClient, volumeName, nil)
}

// mockImageExists sets up mocks for when a registry image already exists.
func mockImageExists(ctx context.Context, mockClient *docker.MockAPIClient) {
	mockClient.EXPECT().
		ImageInspect(ctx, docker.RegistryImageName).
		Return(image.InspectResponse{}, nil).
		Once()
}

// mockImageNotFound sets up mocks for when a registry image is not found.
func mockImageNotFound(ctx context.Context, mockClient *docker.MockAPIClient) {
	mockClient.EXPECT().
		ImageInspect(ctx, docker.RegistryImageName).
		Return(image.InspectResponse{}, errNotFound).
		Once()
}

// mockContainerListError sets up mocks for a ContainerList error.
func mockContainerListError(ctx context.Context, mockClient *docker.MockAPIClient) {
	mockClient.EXPECT().
		ContainerList(ctx, mock.Anything).
		Return(nil, errListFailed).
		Once()
}

// setupDeleteRegistryTest creates the standard setup for delete registry tests.
func setupDeleteRegistryTest(
	t *testing.T,
	state string,
	registryName string,
) (*docker.MockAPIClient, *docker.RegistryManager, context.Context) {
	t.Helper()
	mockClient, manager, ctx := setupTestRegistryManager(t)
	registry := mockRegistryContainer("registry-id", registryName, "test-cluster", state)
	mockContainerListOnce(ctx, mockClient, []container.Summary{registry})

	mockClient.EXPECT().
		ContainerInspect(ctx, "registry-id").
		Return(newInspectResponse(), nil).
		Once()

	if strings.EqualFold(state, "running") {
		mockContainerStopRemove(ctx, mockClient)
	} else {
		mockContainerRemoveOnly(ctx, mockClient, "registry-id")
	}

	return mockClient, manager, ctx
}

func setupRunningDeleteRegistryTest(
	t *testing.T,
	registryName string,
) (*docker.MockAPIClient, *docker.RegistryManager, context.Context) {
	t.Helper()

	mockClient, manager, ctx := setupTestRegistryManager(t)
	registry := mockRegistryContainer("registry-id", registryName, "test-cluster", "running")
	mockContainerListOnce(ctx, mockClient, []container.Summary{registry})

	mockClient.EXPECT().
		ContainerInspect(ctx, "registry-id").
		Return(newInspectResponse(), nil).
		Once()

	return mockClient, manager, ctx
}

func runDeleteRegistryVolumeRemovalTest(
	t *testing.T,
	distinctName string,
	state string,
	expectStop bool,
) {
	t.Helper()

	mockClient, manager, ctx := setupDeleteRegistryTest(t, state, distinctName)

	mockClient.EXPECT().
		VolumeRemove(ctx, deriveVolumeName(distinctName), false).
		Return(nil).
		Once()

	err := manager.DeleteRegistry(ctx, distinctName, "test-cluster", true, "", "")

	require.NoError(t, err)

	if !expectStop {
		mockClient.AssertNotCalled(t, "ContainerStop")
	}
}

// mockContainerCreateStart sets up the container creation and start mock sequence.
func mockContainerCreateStart(
	ctx context.Context, mockClient *docker.MockAPIClient, containerName, containerID string,
) {
	// Mock container create
	mockClient.EXPECT().
		ContainerCreate(
			ctx,
			mock.MatchedBy(func(config *container.Config) bool {
				return config != nil && config.Labels[docker.RegistryLabelKey] == containerName
			}),
			mock.MatchedBy(func(config *container.HostConfig) bool { return config != nil }),
			mock.Anything, // NetworkingConfig can be nil
			mock.Anything,
			containerName,
		).
		Return(container.CreateResponse{ID: containerID}, nil).
		Once()

	// Mock container start
	mockClient.EXPECT().
		ContainerStart(ctx, containerID, mock.Anything).
		Return(nil).
		Once()
}

// mockRegistryContainer returns a mock container summary for a registry.
//

func mockRegistryContainer(registryID, registryName, _ string, state string) container.Summary {
	volumeName := deriveVolumeName(registryName)

	mounts := []container.MountPoint{}
	if volumeName != "" {
		mounts = append(mounts, container.MountPoint{Type: mount.TypeVolume, Name: volumeName})
	}

	return container.Summary{
		ID:     registryID,
		Names:  []string{"/" + registryName},
		Labels: map[string]string{docker.RegistryLabelKey: registryName},
		State:  state,
		Mounts: mounts,
		Image:  docker.RegistryImageName,
	}
}

func newInspectResponse(networks ...string) container.InspectResponse {
	entries := make(map[string]*network.EndpointSettings, len(networks))

	for _, rawName := range networks {
		trimmed := strings.TrimSpace(rawName)
		if trimmed == "" {
			continue
		}

		entries[trimmed] = &network.EndpointSettings{}
	}

	return container.InspectResponse{
		ContainerJSONBase: &container.ContainerJSONBase{},
		NetworkSettings:   &container.NetworkSettings{Networks: entries},
	}
}

// mockContainerStopRemove sets up the container stop and remove mock sequence for "registry-id".
func mockContainerStopRemove(
	ctx context.Context,
	mockClient *docker.MockAPIClient,
) {
	const containerID = "registry-id"
	mockClient.EXPECT().
		ContainerStop(ctx, containerID, mock.MatchedBy(func(_ container.StopOptions) bool { return true })).
		Return(nil).
		Once()

	mockClient.EXPECT().
		ContainerRemove(ctx, containerID, mock.MatchedBy(func(_ container.RemoveOptions) bool { return true })).
		Return(nil).
		Once()
}

// mockContainerRemoveOnly sets up only the container remove expectation.
func mockContainerRemoveOnly(
	ctx context.Context,
	mockClient *docker.MockAPIClient,
	containerID string,
) {
	mockClient.EXPECT().
		ContainerRemove(ctx, containerID, mock.MatchedBy(func(_ container.RemoveOptions) bool { return true })).
		Return(nil).
		Once()
}

func deriveVolumeName(registryName string) string {
	trimmed := strings.TrimSpace(registryName)
	if trimmed == "" {
		return ""
	}

	if strings.HasPrefix(trimmed, "kind-") || strings.HasPrefix(trimmed, "k3d-") {
		parts := strings.SplitN(trimmed, "-", 2)
		if len(parts) == 2 && parts[1] != "" {
			return parts[1]
		}
	}

	return trimmed
}

// mockContainerListOnce sets up a single ContainerList mock with specified registry.
func mockContainerListOnce(
	ctx context.Context, mockClient *docker.MockAPIClient, registries []container.Summary,
) {
	mockClient.EXPECT().
		ContainerList(ctx, mock.Anything).
		Return(registries, nil).
		Once()
}

// mockRegistryContainerWithPort creates a registry container summary with specified port.
func mockRegistryContainerWithPort(registryID, registryName string, port int) container.Summary {
	return container.Summary{
		ID:     registryID,
		Names:  []string{"/" + registryName},
		Labels: map[string]string{docker.RegistryLabelKey: registryName},
		Image:  docker.RegistryImageName,
		Ports: []container.Port{
			{
				PrivatePort: uint16(port), //nolint:gosec // Port values are test constants
				PublicPort:  uint16(port), //nolint:gosec // Port values are test constants
			},
		},
	}
}

func TestNewRegistryManager(t *testing.T) {
	t.Parallel()

	t.Run("success with valid client", func(t *testing.T) {
		t.Parallel()

		mockClient := docker.NewMockAPIClient(t)

		manager, err := docker.NewRegistryManager(mockClient)

		require.NoError(t, err)
		require.NotNil(t, manager)
	})

	t.Run("error with nil client", func(t *testing.T) {
		t.Parallel()

		manager, err := docker.NewRegistryManager(nil)

		require.Error(t, err)
		assert.Nil(t, manager)
		assert.Equal(t, docker.ErrAPIClientNil, err)
	})
}

func TestCreateRegistry(t *testing.T) {
	t.Parallel()

	t.Run("creates new registry successfully", testCreateRegistrySuccess)
	t.Run("shares volume across distributions", testCreateRegistrySharesVolume)
	t.Run("returns nil when registry already exists", testCreateRegistryAlreadyExists)
}

func testCreateRegistrySuccess(t *testing.T) {
	t.Parallel()

	mockClient, manager, ctx := setupTestRegistryManager(t)

	config := docker.RegistryConfig{
		Name:        "docker.io",
		Port:        5000,
		UpstreamURL: "https://registry-1.docker.io",
		ClusterName: "test-cluster",
		NetworkName: "kind",
	}

	mockRegistryNotExists(ctx, mockClient)
	mockImagePullSequence(ctx, mockClient)
	mockVolumeCreateSequence(ctx, mockClient, config.Name)
	mockContainerCreateStart(ctx, mockClient, "docker.io", "test-id")

	err := manager.CreateRegistry(ctx, config)

	require.NoError(t, err)
}

func testCreateRegistrySharesVolume(t *testing.T) {
	t.Parallel()

	mockClient, manager, ctx := setupTestRegistryManager(t)

	config := docker.RegistryConfig{
		Name:        "kind-docker.io",
		Port:        5000,
		UpstreamURL: "https://registry-1.docker.io",
		ClusterName: "test-cluster",
	}

	mockRegistryNotExists(ctx, mockClient)
	mockImagePullSequence(ctx, mockClient)
	mockVolumeCreateSequence(ctx, mockClient, config.Name)
	mockContainerCreateStart(ctx, mockClient, "kind-docker.io", "kind-test-id")

	err := manager.CreateRegistry(ctx, config)

	require.NoError(t, err)
}

func testCreateRegistryAlreadyExists(t *testing.T) {
	t.Parallel()
	mockClient, manager, ctx := setupTestRegistryManager(t)

	config := docker.RegistryConfig{
		Name:        "docker.io",
		ClusterName: "test-cluster",
	}

	// Mock registry exists (called once in registryExists)
	mockClient.EXPECT().
		ContainerList(ctx, mock.Anything).
		Return([]container.Summary{
			{
				ID:     "existing-id",
				Names:  []string{"/docker.io"},
				Labels: map[string]string{docker.RegistryLabelKey: "docker.io"},
				Image:  docker.RegistryImageName,
			},
		}, nil).
		Once()

	err := manager.CreateRegistry(ctx, config)

	require.NoError(t, err)
}

//nolint:funlen // comprehensive test case scenarios for registry deletion
func TestDeleteRegistry(t *testing.T) {
	t.Parallel()

	t.Run("stops running registry before removal", func(t *testing.T) {
		t.Parallel()
		runDeleteRegistryVolumeRemovalTest(t, "docker.io", "running", true)
	})

	t.Run("removes stopped legacy registry without issuing stop", func(t *testing.T) {
		t.Parallel()
		runDeleteRegistryVolumeRemovalTest(t, "kind-docker.io", "exited", false)
	})

	t.Run("returns error when registry not found", func(t *testing.T) {
		t.Parallel()
		mockClient, manager, ctx := setupTestRegistryManager(t)

		mockRegistryNotExists(ctx, mockClient)

		err := manager.DeleteRegistry(ctx, "docker.io", "test-cluster", false, "", "")

		require.Error(t, err)
		assert.Equal(t, docker.ErrRegistryNotFound, err)
	})

	t.Run("removes orphaned explicit volume when registry missing", func(t *testing.T) {
		t.Parallel()
		mockClient, manager, ctx := setupTestRegistryManager(t)

		mockRegistryNotExists(ctx, mockClient)

		mockClient.EXPECT().
			VolumeRemove(ctx, "local-registry", false).
			Return(nil).
			Once()

		err := manager.DeleteRegistry(
			ctx,
			"kind-local-registry",
			"test-cluster",
			true,
			"",
			"local-registry",
		)

		require.ErrorIs(t, err, docker.ErrRegistryNotFound)
	})

	t.Run("falls back to additional orphaned volume candidates", func(t *testing.T) {
		t.Parallel()
		mockClient, manager, ctx := setupTestRegistryManager(t)

		mockRegistryNotExists(ctx, mockClient)

		mockClient.EXPECT().
			VolumeRemove(ctx, "docker.io", false).
			Return(testNotFoundError{}).
			Once()

		mockClient.EXPECT().
			VolumeRemove(ctx, "kind-docker.io", false).
			Return(nil).
			Once()

		err := manager.DeleteRegistry(
			ctx,
			"kind-docker.io",
			"test-cluster",
			true,
			"",
			"",
		)

		require.ErrorIs(t, err, docker.ErrRegistryNotFound)
	})
}

func TestListRegistries(t *testing.T) {
	t.Parallel()

	t.Run("lists all registries", func(t *testing.T) {
		t.Parallel()
		mockClient, manager, ctx := setupTestRegistryManager(t)

		mockContainerListOnce(ctx, mockClient, []container.Summary{
			{
				ID:     "registry1",
				Names:  []string{"/docker.io"},
				Labels: map[string]string{docker.RegistryLabelKey: "docker.io"},
				Image:  docker.RegistryImageName,
			},
			{
				ID:     "registry2",
				Names:  []string{"/quay.io"},
				Labels: map[string]string{docker.RegistryLabelKey: "quay.io"},
				Image:  docker.RegistryImageName,
			},
		})

		registries, err := manager.ListRegistries(ctx)

		require.NoError(t, err)
		assert.Len(t, registries, 2)
		assert.Contains(t, registries, "docker.io")
		assert.Contains(t, registries, "quay.io")
	})

	t.Run("returns empty list when no registries", func(t *testing.T) {
		t.Parallel()
		mockClient, manager, ctx := setupTestRegistryManager(t)

		mockRegistryNotExists(ctx, mockClient)

		registries, err := manager.ListRegistries(ctx)

		require.NoError(t, err)
		assert.Empty(t, registries)
	})
}

func TestIsRegistryInUse(t *testing.T) {
	t.Parallel()

	t.Run("returns true when registry is running", func(t *testing.T) {
		t.Parallel()
		_, manager, ctx := setupRegistryWithState(t, "registry-id", "docker.io", "running")

		inUse, err := manager.IsRegistryInUse(ctx, "docker.io")

		require.NoError(t, err)
		assert.True(t, inUse)
	})

	t.Run("returns false when registry is stopped", func(t *testing.T) {
		t.Parallel()
		_, manager, ctx := setupRegistryWithState(t, "registry-id", "docker.io", "exited")

		inUse, err := manager.IsRegistryInUse(ctx, "docker.io")

		require.NoError(t, err)
		assert.False(t, inUse)
	})

	t.Run("returns false when registry not found", func(t *testing.T) {
		t.Parallel()
		mockClient, manager, ctx := setupTestRegistryManager(t)

		mockRegistryNotExists(ctx, mockClient)

		inUse, err := manager.IsRegistryInUse(ctx, "docker.io")

		require.NoError(t, err)
		assert.False(t, inUse)
	})
}

func TestGetRegistryPort(t *testing.T) {
	t.Parallel()

	t.Run("returns port for existing registry", func(t *testing.T) {
		t.Parallel()
		mockClient, manager, ctx := setupTestRegistryManager(t)

		mockContainerListOnce(ctx, mockClient, []container.Summary{
			mockRegistryContainerWithPort("registry-id", "docker.io", 5000),
		})

		port, err := manager.GetRegistryPort(ctx, "docker.io")

		require.NoError(t, err)
		assert.Equal(t, 5000, port)
	})

	t.Run("returns error when registry not found", func(t *testing.T) {
		t.Parallel()
		mockClient, manager, ctx := setupTestRegistryManager(t)

		mockRegistryNotExists(ctx, mockClient)

		port, err := manager.GetRegistryPort(ctx, "docker.io")

		require.Error(t, err)
		assert.Equal(t, 0, port)
		assert.Equal(t, docker.ErrRegistryNotFound, err)
	})
}

func TestCreateRegistry_ImagePullError(t *testing.T) {
	t.Parallel()

	mockClient, manager, ctx := setupTestRegistryManager(t)

	config := newTestRegistryConfig()

	mockRegistryNotExists(ctx, mockClient)
	mockImageNotFound(ctx, mockClient)

	// Mock image pull failure
	mockClient.EXPECT().
		ImagePull(ctx, docker.RegistryImageName, mock.Anything).
		Return(nil, errImagePullFailed).
		Once()

	err := manager.CreateRegistry(ctx, config)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to ensure registry image")
}

func TestCreateRegistry_VolumeCreateError(t *testing.T) {
	t.Parallel()

	mockClient, manager, ctx, config := setupRegistryCreationTest(t)
	mockImagePullSequence(ctx, mockClient)
	volumeName := mockVolumeInspectMissing(ctx, mockClient, config.Name)
	expectVolumeCreate(ctx, mockClient, volumeName, errVolumeCreateFailed)

	err := manager.CreateRegistry(ctx, config)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create registry volume")
}

func TestCreateRegistry_ContainerCreateError(t *testing.T) {
	t.Parallel()

	mockClient, manager, ctx, config := setupRegistryCreationTest(t)
	mockImagePullSequence(ctx, mockClient)
	mockVolumeCreateSequence(ctx, mockClient, config.Name)

	// Mock container create failure
	mockClient.EXPECT().
		ContainerCreate(ctx, mock.Anything, mock.Anything, mock.Anything, mock.Anything, "docker.io").
		Return(container.CreateResponse{}, errContainerCreateFailed).
		Once()

	err := manager.CreateRegistry(ctx, config)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create registry container")
}

func TestCreateRegistry_ContainerStartError(t *testing.T) {
	t.Parallel()

	mockClient, manager, ctx, config := setupRegistryCreationTest(t)
	mockImagePullSequence(ctx, mockClient)
	mockVolumeCreateSequence(ctx, mockClient, config.Name)

	// Mock container create success
	mockClient.EXPECT().
		ContainerCreate(ctx, mock.Anything, mock.Anything, mock.Anything, mock.Anything, "docker.io").
		Return(container.CreateResponse{ID: "test-id"}, nil).
		Once()

	// Mock container start failure
	mockClient.EXPECT().
		ContainerStart(ctx, "test-id", mock.Anything).
		Return(errContainerStartFailed).
		Once()

	err := manager.CreateRegistry(ctx, config)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to start registry container")
}

func TestCreateRegistry_ImageAlreadyExists(t *testing.T) {
	t.Parallel()

	mockClient, manager, ctx, config := setupRegistryCreationTest(t)
	mockImageExists(ctx, mockClient)

	volumeName := mockVolumeInspectMissing(ctx, mockClient, config.Name)
	expectVolumeCreate(ctx, mockClient, volumeName, nil)
	mockContainerCreateStart(ctx, mockClient, "docker.io", "test-id")

	err := manager.CreateRegistry(ctx, config)

	require.NoError(t, err)
}

func TestCreateRegistry_VolumeAlreadyExists(t *testing.T) {
	t.Parallel()

	mockClient, manager, ctx, config := setupRegistryCreationTest(t)
	mockImagePullSequence(ctx, mockClient)

	volumeName := docker.NormalizeVolumeName(config.Name)

	// Mock volume exists
	mockClient.EXPECT().
		VolumeInspect(ctx, volumeName).
		Return(volume.Volume{}, nil).
		Once()

	mockContainerCreateStart(ctx, mockClient, volumeName, "test-id")

	err := manager.CreateRegistry(ctx, config)

	require.NoError(t, err)
}

func TestDeleteRegistry_ContainerStopError(t *testing.T) {
	t.Parallel()

	mockClient, manager, ctx := setupRunningDeleteRegistryTest(t, "kind-docker.io")

	// Mock container stop failure
	mockClient.EXPECT().
		ContainerStop(ctx, "registry-id", mock.Anything).
		Return(errStopFailed).
		Once()

	err := manager.DeleteRegistry(ctx, "kind-docker.io", "test-cluster", false, "", "")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to stop registry container")
	mockClient.AssertNotCalled(t, "ContainerRemove")
}

func TestDeleteRegistry_ContainerRemoveError(t *testing.T) {
	t.Parallel()

	mockClient, manager, ctx := setupRunningDeleteRegistryTest(t, "kind-docker.io")

	// Mock container stop success
	mockClient.EXPECT().
		ContainerStop(ctx, "registry-id", mock.Anything).
		Return(nil).
		Once()

	// Mock container remove failure
	mockClient.EXPECT().
		ContainerRemove(ctx, "registry-id", mock.Anything).
		Return(errRemoveFailed).
		Once()

	err := manager.DeleteRegistry(ctx, "kind-docker.io", "test-cluster", false, "", "")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to remove registry container")
}

func TestDeleteRegistry_VolumeRemoveError(t *testing.T) {
	t.Parallel()

	mockClient, manager, ctx := setupDeleteRegistryTest(t, "running", "kind-docker.io")

	// Mock volume remove failure
	mockClient.EXPECT().
		VolumeRemove(ctx, "docker.io", false).
		Return(errVolumeRemoveFailed).
		Once()

	err := manager.DeleteRegistry(ctx, "kind-docker.io", "test-cluster", true, "", "")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to remove registry volume")
}

func TestDeleteRegistry_WithoutVolumeDelete(t *testing.T) {
	t.Parallel()

	mockClient, manager, ctx := setupDeleteRegistryTest(t, "running", "kind-docker.io")

	err := manager.DeleteRegistry(ctx, "kind-docker.io", "test-cluster", false, "", "")

	require.NoError(t, err)
	mockClient.AssertNotCalled(t, "VolumeRemove")
}

func TestDeleteRegistry_SkipsRemovalWhenShared(t *testing.T) {
	t.Parallel()

	mockClient, manager, ctx := setupRegistryWithState(t, "registry-id", "docker.io", "running")

	mockClient.EXPECT().
		ContainerInspect(ctx, "registry-id").
		Return(newInspectResponse("k3d-alpha", "k3d-beta"), nil).
		Once()

	mockClient.EXPECT().
		NetworkDisconnect(ctx, "k3d-alpha", "registry-id", true).
		Return(nil).
		Once()

	mockClient.EXPECT().
		ContainerInspect(ctx, "registry-id").
		Return(newInspectResponse("k3d-beta"), nil).
		Once()

	err := manager.DeleteRegistry(ctx, "docker.io", "alpha", false, "k3d-alpha", "")

	require.NoError(t, err)
	mockClient.AssertNotCalled(t, "ContainerStop")
	mockClient.AssertNotCalled(t, "ContainerRemove")
	mockClient.AssertNotCalled(t, "VolumeRemove")
}

func TestListRegistries_Error(t *testing.T) {
	t.Parallel()

	mockClient, manager, ctx := setupTestRegistryManager(t)

	mockContainerListError(ctx, mockClient)

	_, err := manager.ListRegistries(ctx)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to list registry containers")
}

func TestIsRegistryInUse_Error(t *testing.T) {
	t.Parallel()

	mockClient, manager, ctx := setupTestRegistryManager(t)

	mockContainerListError(ctx, mockClient)

	_, err := manager.IsRegistryInUse(ctx, "docker.io")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to list registry containers")
}

func TestGetRegistryPort_NoPortFound(t *testing.T) {
	t.Parallel()

	mockClient, manager, ctx := setupTestRegistryManager(t)

	mockContainerListOnce(ctx, mockClient, []container.Summary{
		mockRegistryContainerWithPort("registry-id", "docker.io", 8080), // Wrong port
	})

	_, err := manager.GetRegistryPort(ctx, "docker.io")

	require.Error(t, err)
	assert.Equal(t, docker.ErrRegistryPortNotFound, err)
}

func TestGetRegistryPort_Error(t *testing.T) {
	t.Parallel()

	mockClient, manager, ctx := setupTestRegistryManager(t)

	mockContainerListError(ctx, mockClient)

	_, err := manager.GetRegistryPort(ctx, "docker.io")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to list registry containers")
}

func TestGetRegistryPort_NotFound(t *testing.T) {
	t.Parallel()

	mockClient, manager, ctx := setupTestRegistryManager(t)

	mockRegistryNotExists(ctx, mockClient)

	_, err := manager.GetRegistryPort(ctx, "docker.io")

	require.Error(t, err)
	assert.Equal(t, docker.ErrRegistryNotFound, err)
}

// errorReader implements io.Reader that returns an error.
type errorReader struct {
	err error
}

func (e *errorReader) Read(_ []byte) (int, error) {
	return 0, e.err
}

type testNotFoundError struct{}

func (testNotFoundError) Error() string {
	return "not found"
}

func (testNotFoundError) NotFound() {}

func TestEnsureRegistryImage_ImagePullReadError(t *testing.T) {
	t.Parallel()

	mockClient, manager, ctx := setupTestRegistryManager(t)

	config := newTestRegistryConfig()

	mockRegistryNotExists(ctx, mockClient)
	mockImageNotFound(ctx, mockClient)

	// Mock image pull with error reader
	errorRdr := &errorReader{err: errReadFailed}
	mockClient.EXPECT().
		ImagePull(ctx, docker.RegistryImageName, mock.Anything).
		Return(io.NopCloser(errorRdr), nil).
		Once()

	err := manager.CreateRegistry(ctx, config)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to ensure registry image")
}
