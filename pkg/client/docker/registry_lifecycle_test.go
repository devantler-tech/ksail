//nolint:err113,funlen // Tests use dynamic errors for mock behaviors and table-driven tests are naturally long
package docker_test

import (
	"errors"
	"testing"

	docker "github.com/devantler-tech/ksail/v7/pkg/client/docker"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestDisconnectFromNetwork(t *testing.T) {
	t.Parallel()

	t.Run("disconnects registry from network successfully", func(t *testing.T) {
		t.Parallel()

		mockClient, manager, ctx := setupTestRegistryManager(t)

		// List registry containers → found
		mockClient.EXPECT().
			ContainerList(ctx, mock.Anything).
			Return([]container.Summary{
				{
					ID:     "reg-id",
					Names:  []string{"/my-registry"},
					Labels: map[string]string{docker.RegistryLabelKey: "my-registry"},
				},
			}, nil).
			Once()

		// Inspect
		mockClient.EXPECT().
			ContainerInspect(ctx, "reg-id").
			Return(container.InspectResponse{
				ContainerJSONBase: &container.ContainerJSONBase{ID: "reg-id"},
				NetworkSettings: &container.NetworkSettings{
					Networks: map[string]*network.EndpointSettings{
						"kind-cluster": {},
					},
				},
			}, nil).
			Once()

		// Disconnect
		mockClient.EXPECT().
			NetworkDisconnect(ctx, "kind-cluster", "reg-id", true).
			Return(nil).
			Once()

		// Re-inspect after disconnect
		mockClient.EXPECT().
			ContainerInspect(ctx, "reg-id").
			Return(container.InspectResponse{
				ContainerJSONBase: &container.ContainerJSONBase{ID: "reg-id"},
				NetworkSettings: &container.NetworkSettings{
					Networks: map[string]*network.EndpointSettings{},
				},
			}, nil).
			Once()

		err := manager.DisconnectFromNetwork(ctx, "my-registry", "kind-cluster")

		require.NoError(t, err)
	})

	t.Run("returns nil when network name is empty", func(t *testing.T) {
		t.Parallel()

		_, manager, ctx := setupTestRegistryManager(t)

		err := manager.DisconnectFromNetwork(ctx, "my-registry", "")

		require.NoError(t, err)
	})

	t.Run("returns nil when network name is whitespace", func(t *testing.T) {
		t.Parallel()

		_, manager, ctx := setupTestRegistryManager(t)

		err := manager.DisconnectFromNetwork(ctx, "my-registry", "   ")

		require.NoError(t, err)
	})

	t.Run("returns nil when registry not found", func(t *testing.T) {
		t.Parallel()

		mockClient, manager, ctx := setupTestRegistryManager(t)

		mockClient.EXPECT().
			ContainerList(ctx, mock.Anything).
			Return([]container.Summary{}, nil).
			Once()

		err := manager.DisconnectFromNetwork(ctx, "missing-registry", "kind-cluster")

		require.NoError(t, err)
	})

	t.Run("returns error when list fails", func(t *testing.T) {
		t.Parallel()

		mockClient, manager, ctx := setupTestRegistryManager(t)

		mockClient.EXPECT().
			ContainerList(ctx, mock.Anything).
			Return(nil, errors.New("docker error")).
			Once()

		err := manager.DisconnectFromNetwork(ctx, "my-registry", "kind-cluster")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to list registry containers")
	})

	t.Run("returns error when inspect fails", func(t *testing.T) {
		t.Parallel()

		mockClient, manager, ctx := setupTestRegistryManager(t)

		mockClient.EXPECT().
			ContainerList(ctx, mock.Anything).
			Return([]container.Summary{
				{
					ID:     "reg-id",
					Labels: map[string]string{docker.RegistryLabelKey: "my-registry"},
				},
			}, nil).
			Once()

		mockClient.EXPECT().
			ContainerInspect(ctx, "reg-id").
			Return(container.InspectResponse{}, errors.New("inspect error")).
			Once()

		err := manager.DisconnectFromNetwork(ctx, "my-registry", "kind-cluster")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to inspect")
	})
}

func TestDisconnectAllFromNetwork(t *testing.T) {
	t.Parallel()

	t.Run("disconnects multiple registries from network", func(t *testing.T) {
		t.Parallel()

		mockClient, manager, ctx := setupTestRegistryManager(t)

		// List all registry containers
		mockClient.EXPECT().
			ContainerList(ctx, mock.Anything).
			Return([]container.Summary{
				{
					ID:     "reg1",
					Names:  []string{"/docker.io"},
					Labels: map[string]string{docker.RegistryLabelKey: "docker.io"},
				},
				{
					ID:     "reg2",
					Names:  []string{"/ghcr.io"},
					Labels: map[string]string{docker.RegistryLabelKey: "ghcr.io"},
				},
			}, nil).
			Once()

		// Inspect reg1 - connected to target network
		mockClient.EXPECT().
			ContainerInspect(ctx, "reg1").
			Return(container.InspectResponse{
				ContainerJSONBase: &container.ContainerJSONBase{ID: "reg1"},
				NetworkSettings: &container.NetworkSettings{
					Networks: map[string]*network.EndpointSettings{
						"kind-cluster": {},
					},
				},
			}, nil).
			Once()

		// Disconnect reg1
		mockClient.EXPECT().
			NetworkDisconnect(ctx, "kind-cluster", "reg1", true).
			Return(nil).
			Once()

		// Re-inspect reg1
		mockClient.EXPECT().
			ContainerInspect(ctx, "reg1").
			Return(container.InspectResponse{
				ContainerJSONBase: &container.ContainerJSONBase{ID: "reg1"},
				NetworkSettings: &container.NetworkSettings{
					Networks: map[string]*network.EndpointSettings{},
				},
			}, nil).
			Once()

		// Inspect reg2 - NOT connected to target network
		mockClient.EXPECT().
			ContainerInspect(ctx, "reg2").
			Return(container.InspectResponse{
				ContainerJSONBase: &container.ContainerJSONBase{ID: "reg2"},
				NetworkSettings: &container.NetworkSettings{
					Networks: map[string]*network.EndpointSettings{
						"k3d-other": {},
					},
				},
			}, nil).
			Once()

		count, err := manager.DisconnectAllFromNetwork(ctx, "kind-cluster")

		require.NoError(t, err)
		assert.Equal(t, 1, count)
	})

	t.Run("returns zero when network is empty", func(t *testing.T) {
		t.Parallel()

		_, manager, ctx := setupTestRegistryManager(t)

		count, err := manager.DisconnectAllFromNetwork(ctx, "")

		require.NoError(t, err)
		assert.Zero(t, count)
	})

	t.Run("returns zero when no containers", func(t *testing.T) {
		t.Parallel()

		mockClient, manager, ctx := setupTestRegistryManager(t)

		mockClient.EXPECT().
			ContainerList(ctx, mock.Anything).
			Return([]container.Summary{}, nil).
			Once()

		count, err := manager.DisconnectAllFromNetwork(ctx, "kind-cluster")

		require.NoError(t, err)
		assert.Zero(t, count)
	})

	t.Run("returns error when list fails", func(t *testing.T) {
		t.Parallel()

		mockClient, manager, ctx := setupTestRegistryManager(t)

		mockClient.EXPECT().
			ContainerList(ctx, mock.Anything).
			Return(nil, errors.New("list error")).
			Once()

		_, err := manager.DisconnectAllFromNetwork(ctx, "kind-cluster")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to list registry containers")
	})

	t.Run("skips container when inspect fails", func(t *testing.T) {
		t.Parallel()

		mockClient, manager, ctx := setupTestRegistryManager(t)

		mockClient.EXPECT().
			ContainerList(ctx, mock.Anything).
			Return([]container.Summary{
				{
					ID:     "reg1",
					Names:  []string{"/docker.io"},
					Labels: map[string]string{docker.RegistryLabelKey: "docker.io"},
				},
			}, nil).
			Once()

		// Inspect fails
		mockClient.EXPECT().
			ContainerInspect(ctx, "reg1").
			Return(container.InspectResponse{}, errors.New("gone")).
			Once()

		count, err := manager.DisconnectAllFromNetwork(ctx, "kind-cluster")

		require.NoError(t, err)
		assert.Zero(t, count)
	})

	t.Run("uses container name from label", func(t *testing.T) {
		t.Parallel()

		mockClient, manager, ctx := setupTestRegistryManager(t)

		mockClient.EXPECT().
			ContainerList(ctx, mock.Anything).
			Return([]container.Summary{
				{
					ID:     "reg1",
					Names:  []string{"/actual-name"},
					Labels: map[string]string{docker.RegistryLabelKey: "label-name"},
				},
			}, nil).
			Once()

		mockClient.EXPECT().
			ContainerInspect(ctx, "reg1").
			Return(container.InspectResponse{
				ContainerJSONBase: &container.ContainerJSONBase{ID: "reg1"},
				NetworkSettings: &container.NetworkSettings{
					Networks: map[string]*network.EndpointSettings{
						"kind-cluster": {},
					},
				},
			}, nil).
			Once()

		mockClient.EXPECT().
			NetworkDisconnect(ctx, "kind-cluster", "reg1", true).
			Return(nil).
			Once()

		mockClient.EXPECT().
			ContainerInspect(ctx, "reg1").
			Return(container.InspectResponse{
				ContainerJSONBase: &container.ContainerJSONBase{ID: "reg1"},
				NetworkSettings: &container.NetworkSettings{
					Networks: map[string]*network.EndpointSettings{},
				},
			}, nil).
			Once()

		count, err := manager.DisconnectAllFromNetwork(ctx, "kind-cluster")

		require.NoError(t, err)
		assert.Equal(t, 1, count)
	})

	t.Run("returns error on disconnect failure", func(t *testing.T) {
		t.Parallel()

		mockClient, manager, ctx := setupTestRegistryManager(t)

		mockClient.EXPECT().
			ContainerList(ctx, mock.Anything).
			Return([]container.Summary{
				{
					ID:     "reg1",
					Names:  []string{"/docker.io"},
					Labels: map[string]string{docker.RegistryLabelKey: "docker.io"},
				},
			}, nil).
			Once()

		mockClient.EXPECT().
			ContainerInspect(ctx, "reg1").
			Return(container.InspectResponse{
				ContainerJSONBase: &container.ContainerJSONBase{ID: "reg1"},
				NetworkSettings: &container.NetworkSettings{
					Networks: map[string]*network.EndpointSettings{
						"kind-cluster": {},
					},
				},
			}, nil).
			Once()

		mockClient.EXPECT().
			NetworkDisconnect(ctx, "kind-cluster", "reg1", true).
			Return(errors.New("unexpected disconnect error")).
			Once()

		_, err := manager.DisconnectAllFromNetwork(ctx, "kind-cluster")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to disconnect")
	})
}

func TestListRegistriesOnNetwork(t *testing.T) {
	t.Parallel()

	t.Run("returns registries connected to network", func(t *testing.T) {
		t.Parallel()

		mockClient, manager, ctx := setupTestRegistryManager(t)

		// List all registry image containers
		mockClient.EXPECT().
			ContainerList(ctx, mock.Anything).
			Return([]container.Summary{
				{
					ID:     "reg1",
					Names:  []string{"/docker-mirror"},
					Labels: map[string]string{docker.RegistryLabelKey: "docker-mirror"},
				},
				{
					ID:    "reg2",
					Names: []string{"/k3d-registry"},
					// No KSail label
				},
			}, nil).
			Once()

		// Inspect reg1 - connected to target network
		mockClient.EXPECT().
			ContainerInspect(ctx, "reg1").
			Return(container.InspectResponse{
				ContainerJSONBase: &container.ContainerJSONBase{ID: "reg1"},
				NetworkSettings: &container.NetworkSettings{
					Networks: map[string]*network.EndpointSettings{
						"kind-cluster": {},
					},
				},
			}, nil).
			Once()

		// Inspect reg2 - also connected
		mockClient.EXPECT().
			ContainerInspect(ctx, "reg2").
			Return(container.InspectResponse{
				ContainerJSONBase: &container.ContainerJSONBase{ID: "reg2"},
				NetworkSettings: &container.NetworkSettings{
					Networks: map[string]*network.EndpointSettings{
						"kind-cluster": {},
					},
				},
			}, nil).
			Once()

		registries, err := manager.ListRegistriesOnNetwork(ctx, "kind-cluster")

		require.NoError(t, err)
		require.Len(t, registries, 2)
		assert.Equal(t, "docker-mirror", registries[0].Name)
		assert.True(t, registries[0].IsKSailOwned)
		assert.Equal(t, "k3d-registry", registries[1].Name)
		assert.False(t, registries[1].IsKSailOwned)
	})

	t.Run("returns nil for empty network name", func(t *testing.T) {
		t.Parallel()

		_, manager, ctx := setupTestRegistryManager(t)

		registries, err := manager.ListRegistriesOnNetwork(ctx, "")

		require.NoError(t, err)
		assert.Nil(t, registries)
	})

	t.Run("returns nil for whitespace network name", func(t *testing.T) {
		t.Parallel()

		_, manager, ctx := setupTestRegistryManager(t)

		registries, err := manager.ListRegistriesOnNetwork(ctx, "   ")

		require.NoError(t, err)
		assert.Nil(t, registries)
	})

	t.Run("filters out containers not on target network", func(t *testing.T) {
		t.Parallel()

		mockClient, manager, ctx := setupTestRegistryManager(t)

		mockClient.EXPECT().
			ContainerList(ctx, mock.Anything).
			Return([]container.Summary{
				{
					ID:     "reg1",
					Names:  []string{"/docker-mirror"},
					Labels: map[string]string{docker.RegistryLabelKey: "docker-mirror"},
				},
			}, nil).
			Once()

		// reg1 is on a different network
		mockClient.EXPECT().
			ContainerInspect(ctx, "reg1").
			Return(container.InspectResponse{
				ContainerJSONBase: &container.ContainerJSONBase{ID: "reg1"},
				NetworkSettings: &container.NetworkSettings{
					Networks: map[string]*network.EndpointSettings{
						"k3d-other": {},
					},
				},
			}, nil).
			Once()

		registries, err := manager.ListRegistriesOnNetwork(ctx, "kind-cluster")

		require.NoError(t, err)
		assert.Empty(t, registries)
	})

	t.Run("returns error when container list fails", func(t *testing.T) {
		t.Parallel()

		mockClient, manager, ctx := setupTestRegistryManager(t)

		mockClient.EXPECT().
			ContainerList(ctx, mock.Anything).
			Return(nil, errors.New("list error")).
			Once()

		_, err := manager.ListRegistriesOnNetwork(ctx, "kind-cluster")

		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to list registry containers")
	})

	t.Run("skips container when inspect fails", func(t *testing.T) {
		t.Parallel()

		mockClient, manager, ctx := setupTestRegistryManager(t)

		mockClient.EXPECT().
			ContainerList(ctx, mock.Anything).
			Return([]container.Summary{
				{
					ID:     "reg1",
					Names:  []string{"/registry"},
					Labels: map[string]string{docker.RegistryLabelKey: "registry"},
				},
			}, nil).
			Once()

		mockClient.EXPECT().
			ContainerInspect(ctx, "reg1").
			Return(container.InspectResponse{}, errors.New("inspect error")).
			Once()

		registries, err := manager.ListRegistriesOnNetwork(ctx, "kind-cluster")

		require.NoError(t, err)
		assert.Empty(t, registries)
	})
}

func TestDeleteRegistriesOnNetwork(t *testing.T) {
	t.Parallel()

	t.Run("deletes registries on network", func(t *testing.T) {
		t.Parallel()

		mockClient, manager, ctx := setupTestRegistryManager(t)

		// ListRegistriesOnNetwork → list all registry image containers
		mockClient.EXPECT().
			ContainerList(ctx, mock.Anything).
			Return([]container.Summary{
				{
					ID:     "reg1",
					Names:  []string{"/docker-mirror"},
					Labels: map[string]string{docker.RegistryLabelKey: "docker-mirror"},
				},
			}, nil).
			Once()

		// Inspect for ListRegistriesOnNetwork
		mockClient.EXPECT().
			ContainerInspect(ctx, "reg1").
			Return(container.InspectResponse{
				ContainerJSONBase: &container.ContainerJSONBase{ID: "reg1"},
				NetworkSettings: &container.NetworkSettings{
					Networks: map[string]*network.EndpointSettings{
						"kind-cluster": {},
					},
				},
			}, nil).
			Once()

		// DisconnectFromNetwork → list registry containers
		mockClient.EXPECT().
			ContainerList(ctx, mock.Anything).
			Return([]container.Summary{
				{
					ID:     "reg1",
					Names:  []string{"/docker-mirror"},
					Labels: map[string]string{docker.RegistryLabelKey: "docker-mirror"},
				},
			}, nil).
			Once()

		// Inspect for DisconnectFromNetwork
		mockClient.EXPECT().
			ContainerInspect(ctx, "reg1").
			Return(container.InspectResponse{
				ContainerJSONBase: &container.ContainerJSONBase{ID: "reg1"},
				NetworkSettings: &container.NetworkSettings{
					Networks: map[string]*network.EndpointSettings{
						"kind-cluster": {},
					},
				},
			}, nil).
			Once()

		// NetworkDisconnect
		mockClient.EXPECT().
			NetworkDisconnect(ctx, "kind-cluster", "reg1", true).
			Return(nil).
			Once()

		// Re-inspect after disconnect
		mockClient.EXPECT().
			ContainerInspect(ctx, "reg1").
			Return(container.InspectResponse{
				ContainerJSONBase: &container.ContainerJSONBase{ID: "reg1"},
				NetworkSettings: &container.NetworkSettings{
					Networks: map[string]*network.EndpointSettings{},
				},
			}, nil).
			Once()

		// deleteRegistryContainer: stop + remove
		mockClient.EXPECT().
			ContainerStop(ctx, "reg1", mock.Anything).
			Return(nil).
			Once()

		mockClient.EXPECT().
			ContainerRemove(ctx, "reg1", mock.Anything).
			Return(nil).
			Once()

		deleted, err := manager.DeleteRegistriesOnNetwork(ctx, "kind-cluster", false)

		require.NoError(t, err)
		assert.Equal(t, []string{"docker-mirror"}, deleted)
	})

	t.Run("returns empty when no registries on network", func(t *testing.T) {
		t.Parallel()

		mockClient, manager, ctx := setupTestRegistryManager(t)

		// No containers found
		mockClient.EXPECT().
			ContainerList(ctx, mock.Anything).
			Return([]container.Summary{}, nil).
			Once()

		deleted, err := manager.DeleteRegistriesOnNetwork(ctx, "kind-cluster", false)

		require.NoError(t, err)
		assert.Empty(t, deleted)
	})

	t.Run("skips registry when disconnect fails", func(t *testing.T) {
		t.Parallel()

		mockClient, manager, ctx := setupTestRegistryManager(t)

		// ListRegistriesOnNetwork
		mockClient.EXPECT().
			ContainerList(ctx, mock.Anything).
			Return([]container.Summary{
				{
					ID:     "reg1",
					Names:  []string{"/docker-mirror"},
					Labels: map[string]string{docker.RegistryLabelKey: "docker-mirror"},
				},
			}, nil).
			Once()

		// Inspect for ListRegistriesOnNetwork
		mockClient.EXPECT().
			ContainerInspect(ctx, "reg1").
			Return(container.InspectResponse{
				ContainerJSONBase: &container.ContainerJSONBase{ID: "reg1"},
				NetworkSettings: &container.NetworkSettings{
					Networks: map[string]*network.EndpointSettings{
						"kind-cluster": {},
					},
				},
			}, nil).
			Once()

		// DisconnectFromNetwork → list fails
		mockClient.EXPECT().
			ContainerList(ctx, mock.Anything).
			Return(nil, errors.New("list error")).
			Once()

		deleted, err := manager.DeleteRegistriesOnNetwork(ctx, "kind-cluster", false)

		require.NoError(t, err)
		assert.Empty(t, deleted, "should skip this registry due to disconnect failure")
	})
}

func TestDeleteRegistriesByInfo(t *testing.T) {
	t.Parallel()

	t.Run("deletes provided registries", func(t *testing.T) {
		t.Parallel()

		mockClient, manager, ctx := setupTestRegistryManager(t)

		registries := []docker.RegistryInfo{
			{Name: "docker-mirror", ID: "reg1", IsKSailOwned: true},
			{Name: "ghcr-mirror", ID: "reg2", IsKSailOwned: false},
		}

		// Stop + remove reg1
		mockClient.EXPECT().
			ContainerStop(ctx, "reg1", mock.Anything).
			Return(nil).
			Once()
		mockClient.EXPECT().
			ContainerRemove(ctx, "reg1", mock.Anything).
			Return(nil).
			Once()

		// Stop + remove reg2
		mockClient.EXPECT().
			ContainerStop(ctx, "reg2", mock.Anything).
			Return(nil).
			Once()
		mockClient.EXPECT().
			ContainerRemove(ctx, "reg2", mock.Anything).
			Return(nil).
			Once()

		deleted, err := manager.DeleteRegistriesByInfo(ctx, registries, false)

		require.NoError(t, err)
		assert.Len(t, deleted, 2)
		assert.Contains(t, deleted, "docker-mirror")
		assert.Contains(t, deleted, "ghcr-mirror")
	})

	t.Run("skips registry that fails to stop", func(t *testing.T) {
		t.Parallel()

		mockClient, manager, ctx := setupTestRegistryManager(t)

		registries := []docker.RegistryInfo{
			{Name: "docker-mirror", ID: "reg1"},
		}

		mockClient.EXPECT().
			ContainerStop(ctx, "reg1", mock.Anything).
			Return(errors.New("stop failed")).
			Once()

		deleted, err := manager.DeleteRegistriesByInfo(ctx, registries, false)

		require.NoError(t, err)
		assert.Empty(t, deleted)
	})

	t.Run("skips registry that fails to remove", func(t *testing.T) {
		t.Parallel()

		mockClient, manager, ctx := setupTestRegistryManager(t)

		registries := []docker.RegistryInfo{
			{Name: "docker-mirror", ID: "reg1"},
		}

		mockClient.EXPECT().
			ContainerStop(ctx, "reg1", mock.Anything).
			Return(nil).
			Once()

		mockClient.EXPECT().
			ContainerRemove(ctx, "reg1", mock.Anything).
			Return(errors.New("remove failed")).
			Once()

		deleted, err := manager.DeleteRegistriesByInfo(ctx, registries, false)

		require.NoError(t, err)
		assert.Empty(t, deleted)
	})

	t.Run("returns empty for empty input", func(t *testing.T) {
		t.Parallel()

		_, manager, ctx := setupTestRegistryManager(t)

		deleted, err := manager.DeleteRegistriesByInfo(ctx, nil, false)

		require.NoError(t, err)
		assert.Empty(t, deleted)
	})

	t.Run("deletes with volumes", func(t *testing.T) {
		t.Parallel()

		mockClient, manager, ctx := setupTestRegistryManager(t)

		registries := []docker.RegistryInfo{
			{Name: "docker-mirror", ID: "reg1"},
		}

		// Inspect for volume discovery
		mockClient.EXPECT().
			ContainerInspect(ctx, "reg1").
			Return(container.InspectResponse{
				ContainerJSONBase: &container.ContainerJSONBase{ID: "reg1"},
				Mounts: []container.MountPoint{
					{
						Destination: docker.RegistryDataPath,
						Name:        "registry-data-vol",
					},
				},
			}, nil).
			Once()

		// Stop + remove
		mockClient.EXPECT().
			ContainerStop(ctx, "reg1", mock.Anything).
			Return(nil).
			Once()
		mockClient.EXPECT().
			ContainerRemove(ctx, "reg1", mock.Anything).
			Return(nil).
			Once()

		// Volume remove
		mockClient.EXPECT().
			VolumeRemove(ctx, "registry-data-vol", false).
			Return(nil).
			Once()

		deleted, err := manager.DeleteRegistriesByInfo(ctx, registries, true)

		require.NoError(t, err)
		assert.Equal(t, []string{"docker-mirror"}, deleted)
	})
}

func TestListRegistries_DeduplicatesNames(t *testing.T) {
	t.Parallel()

	mockClient, manager, ctx := setupTestRegistryManager(t)

	mockClient.EXPECT().
		ContainerList(ctx, mock.Anything).
		Return([]container.Summary{
			{
				ID:     "r1",
				Names:  []string{"/docker.io"},
				Labels: map[string]string{docker.RegistryLabelKey: "docker.io"},
			},
			{
				ID:     "r2",
				Names:  []string{"/docker.io"},
				Labels: map[string]string{docker.RegistryLabelKey: "docker.io"},
			},
		}, nil).
		Once()

	registries, err := manager.ListRegistries(ctx)

	require.NoError(t, err)
	assert.Len(t, registries, 1)
	assert.Equal(t, "docker.io", registries[0])
}

func TestListRegistries_FallbackToContainerName(t *testing.T) {
	t.Parallel()

	mockClient, manager, ctx := setupTestRegistryManager(t)

	mockClient.EXPECT().
		ContainerList(ctx, mock.Anything).
		Return([]container.Summary{
			{
				ID:     "r1",
				Names:  []string{"/unnamed-registry"},
				Labels: map[string]string{},
			},
		}, nil).
		Once()

	registries, err := manager.ListRegistries(ctx)

	require.NoError(t, err)
	assert.Len(t, registries, 1)
	assert.Equal(t, "unnamed-registry", registries[0])
}

func TestListRegistries_SkipsContainerWithNoName(t *testing.T) {
	t.Parallel()

	mockClient, manager, ctx := setupTestRegistryManager(t)

	mockClient.EXPECT().
		ContainerList(ctx, mock.Anything).
		Return([]container.Summary{
			{
				ID:     "r1",
				Names:  []string{},
				Labels: map[string]string{},
			},
		}, nil).
		Once()

	registries, err := manager.ListRegistries(ctx)

	require.NoError(t, err)
	assert.Empty(t, registries)
}
