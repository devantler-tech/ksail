// Package docker_test provides unit tests for the docker package.
//
//nolint:err113,funlen // Tests use dynamic errors for mock behaviors and table-driven tests are naturally long
package docker_test

import (
	"context"
	"errors"
	"testing"

	docker "github.com/devantler-tech/ksail/v5/pkg/client/docker"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test constants.
const (
	testContainerID   = "test-container-id"
	testClusterKind   = "kind-test-cluster"
	testRegistryName  = "docker.io"
	testFallbackName  = "fallback-name"
	testContainerName = "test-container"
)

func TestUniqueNonEmpty(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    []string
		expected []string
	}{
		{
			name:     "empty input returns empty slice",
			input:    []string{},
			expected: []string{},
		},
		{
			name:     "single value returns single value",
			input:    []string{"foo"},
			expected: []string{"foo"},
		},
		{
			name:     "duplicate values are removed",
			input:    []string{"foo", "bar", "foo"},
			expected: []string{"foo", "bar"},
		},
		{
			name:     "empty strings are filtered out",
			input:    []string{"foo", "", "bar"},
			expected: []string{"foo", "bar"},
		},
		{
			name:     "whitespace only strings are filtered out",
			input:    []string{"foo", "   ", "bar"},
			expected: []string{"foo", "bar"},
		},
		{
			name:     "whitespace is trimmed from values",
			input:    []string{"  foo  ", "  bar  "},
			expected: []string{"foo", "bar"},
		},
		{
			name:     "duplicates after trimming are removed",
			input:    []string{"  foo", "foo  ", "foo"},
			expected: []string{"foo"},
		},
		{
			name:     "all empty values returns empty slice",
			input:    []string{"", "   ", "\t"},
			expected: []string{},
		},
		{
			name:     "preserves order of first occurrence",
			input:    []string{"c", "a", "b", "a", "c"},
			expected: []string{"c", "a", "b"},
		},
		{
			name:     "nil input returns empty slice",
			input:    nil,
			expected: []string{},
		},
	}

	for i := range tests {
		testCase := tests[i]

		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := docker.UniqueNonEmpty(testCase.input...)

			assert.Equal(t, testCase.expected, result)
		})
	}
}

func TestIsNotConnectedError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{
			name:     "nil error returns false",
			err:      nil,
			expected: false,
		},
		{
			name:     "not connected error returns true",
			err:      errors.New("container xyz is not connected to the network abc"),
			expected: true,
		},
		{
			name:     "random error returns false",
			err:      errors.New("something went wrong"),
			expected: false,
		},
		{
			name:     "partial match returns true",
			err:      errors.New("Error: is not connected to the network"),
			expected: true,
		},
		{
			name:     "case sensitive - uppercase returns false",
			err:      errors.New("container IS NOT CONNECTED TO THE NETWORK"),
			expected: false,
		},
		{
			name:     "wrapped error with not connected message returns true",
			err:      errors.New("wrapped: is not connected to the network: details"),
			expected: true,
		},
	}

	for i := range tests {
		testCase := tests[i]

		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := docker.IsNotConnectedError(testCase.err)

			assert.Equal(t, testCase.expected, result)
		})
	}
}

func TestIsClusterNetworkName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		network  string
		expected bool
	}{
		{
			name:     "empty string returns false",
			network:  "",
			expected: false,
		},
		{
			name:     "kind network returns true",
			network:  "kind",
			expected: true,
		},
		{
			name:     "kind-prefixed network returns true",
			network:  "kind-test-cluster",
			expected: true,
		},
		{
			name:     "k3d network returns true",
			network:  "k3d",
			expected: true,
		},
		{
			name:     "k3d-prefixed network returns true",
			network:  "k3d-test-cluster",
			expected: true,
		},
		{
			name:     "bridge network returns false",
			network:  "bridge",
			expected: false,
		},
		{
			name:     "host network returns false",
			network:  "host",
			expected: false,
		},
		{
			name:     "custom network returns false",
			network:  "my-custom-network",
			expected: false,
		},
		{
			name:     "kubernetes network returns false",
			network:  "kubernetes",
			expected: false,
		},
		{
			name:     "similar but not matching prefix returns false",
			network:  "kindof-network",
			expected: false,
		},
		{
			name:     "k3d alone is cluster network",
			network:  "k3d",
			expected: true,
		},
	}

	for i := range tests {
		testCase := tests[i]

		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := docker.IsClusterNetworkName(testCase.network)

			assert.Equal(t, testCase.expected, result)
		})
	}
}

func TestRegistryAttachedToOtherClusters(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		inspect        container.InspectResponse
		ignoredNetwork string
		expected       bool
	}{
		{
			name: "nil network settings returns false",
			inspect: container.InspectResponse{
				NetworkSettings: nil,
			},
			ignoredNetwork: "",
			expected:       false,
		},
		{
			name: "empty networks returns false",
			inspect: container.InspectResponse{
				NetworkSettings: &container.NetworkSettings{
					Networks: map[string]*network.EndpointSettings{},
				},
			},
			ignoredNetwork: "",
			expected:       false,
		},
		{
			name: "only non-cluster networks returns false",
			inspect: container.InspectResponse{
				NetworkSettings: &container.NetworkSettings{
					Networks: map[string]*network.EndpointSettings{
						"bridge": {},
						"host":   {},
					},
				},
			},
			ignoredNetwork: "",
			expected:       false,
		},
		{
			name: "cluster network present returns true",
			inspect: container.InspectResponse{
				NetworkSettings: &container.NetworkSettings{
					Networks: map[string]*network.EndpointSettings{
						"kind-test-cluster": {},
					},
				},
			},
			ignoredNetwork: "",
			expected:       true,
		},
		{
			name: "only ignored cluster network returns false",
			inspect: container.InspectResponse{
				NetworkSettings: &container.NetworkSettings{
					Networks: map[string]*network.EndpointSettings{
						"kind-test-cluster": {},
					},
				},
			},
			ignoredNetwork: "kind-test-cluster",
			expected:       false,
		},
		{
			name: "multiple cluster networks with one ignored returns true",
			inspect: container.InspectResponse{
				NetworkSettings: &container.NetworkSettings{
					Networks: map[string]*network.EndpointSettings{
						"kind-cluster1": {},
						"kind-cluster2": {},
					},
				},
			},
			ignoredNetwork: "kind-cluster1",
			expected:       true,
		},
		{
			name: "ignoring case insensitive",
			inspect: container.InspectResponse{
				NetworkSettings: &container.NetworkSettings{
					Networks: map[string]*network.EndpointSettings{
						"Kind-Test-Cluster": {},
					},
				},
			},
			ignoredNetwork: "kind-test-cluster",
			expected:       false,
		},
		{
			name: "empty string network name in map is skipped",
			inspect: container.InspectResponse{
				NetworkSettings: &container.NetworkSettings{
					Networks: map[string]*network.EndpointSettings{
						"":                  {},
						"kind-test-cluster": {},
					},
				},
			},
			ignoredNetwork: "",
			expected:       true,
		},
		{
			name: "whitespace network name in map is skipped",
			inspect: container.InspectResponse{
				NetworkSettings: &container.NetworkSettings{
					Networks: map[string]*network.EndpointSettings{
						"   ":               {},
						"kind-test-cluster": {},
					},
				},
			},
			ignoredNetwork: "",
			expected:       true,
		},
		{
			name: "k3d network also detected as cluster network",
			inspect: container.InspectResponse{
				NetworkSettings: &container.NetworkSettings{
					Networks: map[string]*network.EndpointSettings{
						"k3d-my-cluster": {},
					},
				},
			},
			ignoredNetwork: "",
			expected:       true,
		},
	}

	for i := range tests {
		testCase := tests[i]

		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := docker.RegistryAttachedToOtherClusters(
				testCase.inspect,
				testCase.ignoredNetwork,
			)

			assert.Equal(t, testCase.expected, result)
		})
	}
}

func TestDeriveRegistryVolumeName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		registry container.Summary
		fallback string
		expected string
	}{
		{
			name: "returns volume name from mount point",
			registry: container.Summary{
				Mounts: []container.MountPoint{
					{Type: mount.TypeVolume, Name: "registry-volume"},
				},
			},
			fallback: "fallback-name",
			expected: "registry-volume",
		},
		{
			name: "skips bind mounts",
			registry: container.Summary{
				Mounts: []container.MountPoint{
					{Type: mount.TypeBind, Name: "bind-mount"},
					{Type: mount.TypeVolume, Name: "volume-mount"},
				},
			},
			fallback: "fallback-name",
			expected: "volume-mount",
		},
		{
			name: "returns fallback when no volume mounts",
			registry: container.Summary{
				Mounts: []container.MountPoint{},
			},
			fallback: "fallback-name",
			expected: "fallback-name",
		},
		{
			name: "returns normalized fallback for kind prefix",
			registry: container.Summary{
				Mounts: []container.MountPoint{},
			},
			fallback: "kind-docker.io",
			expected: "docker.io",
		},
		{
			name: "returns normalized fallback for k3d prefix",
			registry: container.Summary{
				Mounts: []container.MountPoint{},
			},
			fallback: "k3d-registry",
			expected: "registry",
		},
		{
			name: "handles empty volume name in mount",
			registry: container.Summary{
				Mounts: []container.MountPoint{
					{Type: mount.TypeVolume, Name: ""},
					{Type: mount.TypeVolume, Name: "actual-volume"},
				},
			},
			fallback: "fallback-name",
			expected: "actual-volume",
		},
		{
			name: "returns trimmed fallback",
			registry: container.Summary{
				Mounts: []container.MountPoint{},
			},
			fallback: "  whitespace-fallback  ",
			expected: "whitespace-fallback",
		},
		{
			name: "returns empty string when no mounts and empty fallback",
			registry: container.Summary{
				Mounts: []container.MountPoint{},
			},
			fallback: "",
			expected: "",
		},
	}

	for i := range tests {
		testCase := tests[i]

		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := docker.DeriveRegistryVolumeName(testCase.registry, testCase.fallback)

			assert.Equal(t, testCase.expected, result)
		})
	}
}

func TestInspectContainer(t *testing.T) {
	t.Parallel()

	t.Run("returns container inspection response", func(t *testing.T) {
		t.Parallel()

		mockClient := docker.NewMockAPIClient(t)
		ctx := context.Background()
		containerID := testContainerID

		expectedInspect := container.InspectResponse{
			ContainerJSONBase: &container.ContainerJSONBase{
				ID:   containerID,
				Name: "test-container",
			},
		}

		mockClient.EXPECT().
			ContainerInspect(ctx, containerID).
			Return(expectedInspect, nil).
			Once()

		result, err := docker.InspectContainer(ctx, mockClient, containerID)

		require.NoError(t, err)
		assert.Equal(t, containerID, result.ID)
		assert.Equal(t, "test-container", result.Name)
	})

	t.Run("returns error when inspection fails", func(t *testing.T) {
		t.Parallel()

		mockClient := docker.NewMockAPIClient(t)
		ctx := context.Background()
		containerID := testContainerID

		mockClient.EXPECT().
			ContainerInspect(ctx, containerID).
			Return(container.InspectResponse{}, errors.New("inspection failed")).
			Once()

		_, err := docker.InspectContainer(ctx, mockClient, containerID)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to inspect registry container")
	})
}

func TestDisconnectRegistryNetwork(t *testing.T) {
	t.Parallel()

	t.Run("disconnects successfully", func(t *testing.T) {
		t.Parallel()

		mockClient := docker.NewMockAPIClient(t)
		ctx := context.Background()
		containerID := testContainerID
		networkName := testClusterKind

		inspectBefore := container.InspectResponse{
			ContainerJSONBase: &container.ContainerJSONBase{ID: containerID},
			NetworkSettings: &container.NetworkSettings{
				Networks: map[string]*network.EndpointSettings{
					networkName: {},
				},
			},
		}

		inspectAfter := container.InspectResponse{
			ContainerJSONBase: &container.ContainerJSONBase{ID: containerID},
			NetworkSettings: &container.NetworkSettings{
				Networks: map[string]*network.EndpointSettings{},
			},
		}

		mockClient.EXPECT().
			NetworkDisconnect(ctx, networkName, containerID, true).
			Return(nil).
			Once()

		mockClient.EXPECT().
			ContainerInspect(ctx, containerID).
			Return(inspectAfter, nil).
			Once()

		result, err := docker.DisconnectRegistryNetwork(
			ctx,
			mockClient,
			containerID,
			"docker.io",
			networkName,
			inspectBefore,
		)

		require.NoError(t, err)
		assert.Empty(t, result.NetworkSettings.Networks)
	})

	t.Run("returns early when network is empty", func(t *testing.T) {
		t.Parallel()

		mockClient := docker.NewMockAPIClient(t)
		ctx := context.Background()
		containerID := testContainerID

		inspectInput := container.InspectResponse{
			ContainerJSONBase: &container.ContainerJSONBase{ID: containerID},
		}

		result, err := docker.DisconnectRegistryNetwork(
			ctx,
			mockClient,
			containerID,
			"docker.io",
			"",
			inspectInput,
		)

		require.NoError(t, err)
		assert.Equal(t, inspectInput, result)
		mockClient.AssertNotCalled(t, "NetworkDisconnect")
	})

	t.Run("ignores not connected error", func(t *testing.T) {
		t.Parallel()

		mockClient := docker.NewMockAPIClient(t)
		ctx := context.Background()
		containerID := testContainerID
		networkName := testClusterKind

		inspectInput := container.InspectResponse{
			ContainerJSONBase: &container.ContainerJSONBase{ID: containerID},
		}

		inspectAfter := container.InspectResponse{
			ContainerJSONBase: &container.ContainerJSONBase{ID: containerID},
			NetworkSettings: &container.NetworkSettings{
				Networks: map[string]*network.EndpointSettings{},
			},
		}

		mockClient.EXPECT().
			NetworkDisconnect(ctx, networkName, containerID, true).
			Return(errors.New("container is not connected to the network")).
			Once()

		mockClient.EXPECT().
			ContainerInspect(ctx, containerID).
			Return(inspectAfter, nil).
			Once()

		result, err := docker.DisconnectRegistryNetwork(
			ctx,
			mockClient,
			containerID,
			"docker.io",
			networkName,
			inspectInput,
		)

		require.NoError(t, err)
		assert.NotNil(t, result.NetworkSettings)
	})

	t.Run("ignores not found error", func(t *testing.T) {
		t.Parallel()

		mockClient := docker.NewMockAPIClient(t)
		ctx := context.Background()
		containerID := testContainerID
		networkName := testClusterKind

		inspectInput := container.InspectResponse{
			ContainerJSONBase: &container.ContainerJSONBase{ID: containerID},
		}

		inspectAfter := container.InspectResponse{
			ContainerJSONBase: &container.ContainerJSONBase{ID: containerID},
			NetworkSettings: &container.NetworkSettings{
				Networks: map[string]*network.EndpointSettings{},
			},
		}

		mockClient.EXPECT().
			NetworkDisconnect(ctx, networkName, containerID, true).
			Return(testNotFoundError{}).
			Once()

		mockClient.EXPECT().
			ContainerInspect(ctx, containerID).
			Return(inspectAfter, nil).
			Once()

		result, err := docker.DisconnectRegistryNetwork(
			ctx,
			mockClient,
			containerID,
			"docker.io",
			networkName,
			inspectInput,
		)

		require.NoError(t, err)
		assert.NotNil(t, result.NetworkSettings)
	})

	t.Run("returns error on unexpected disconnect failure", func(t *testing.T) {
		t.Parallel()

		mockClient := docker.NewMockAPIClient(t)
		ctx := context.Background()
		containerID := testContainerID
		networkName := testClusterKind

		inspectInput := container.InspectResponse{
			ContainerJSONBase: &container.ContainerJSONBase{ID: containerID},
		}

		mockClient.EXPECT().
			NetworkDisconnect(ctx, networkName, containerID, true).
			Return(errors.New("unexpected error")).
			Once()

		_, err := docker.DisconnectRegistryNetwork(
			ctx,
			mockClient,
			containerID,
			"docker.io",
			networkName,
			inspectInput,
		)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to disconnect registry")
	})
}

func TestCleanupRegistryVolume(t *testing.T) {
	t.Parallel()

	t.Run("does nothing when deleteVolume is false", func(t *testing.T) {
		t.Parallel()

		mockClient := docker.NewMockAPIClient(t)
		ctx := context.Background()
		registry := container.Summary{}

		err := docker.CleanupRegistryVolume(ctx, mockClient, registry, "", "fallback", false)

		require.NoError(t, err)
		mockClient.AssertNotCalled(t, "VolumeRemove")
	})

	t.Run("removes explicit volume when provided", func(t *testing.T) {
		t.Parallel()

		mockClient := docker.NewMockAPIClient(t)
		ctx := context.Background()
		registry := container.Summary{}

		mockClient.EXPECT().
			VolumeRemove(ctx, "explicit-volume", false).
			Return(nil).
			Once()

		err := docker.CleanupRegistryVolume(
			ctx,
			mockClient,
			registry,
			"explicit-volume",
			"fallback",
			true,
		)

		require.NoError(t, err)
	})

	t.Run("derives volume name from registry when no explicit volume", func(t *testing.T) {
		t.Parallel()

		mockClient := docker.NewMockAPIClient(t)
		ctx := context.Background()
		registry := container.Summary{
			Mounts: []container.MountPoint{
				{Type: mount.TypeVolume, Name: "registry-volume"},
			},
		}

		mockClient.EXPECT().
			VolumeRemove(ctx, "registry-volume", false).
			Return(nil).
			Once()

		err := docker.CleanupRegistryVolume(ctx, mockClient, registry, "", "fallback", true)

		require.NoError(t, err)
	})

	t.Run("returns error on volume removal failure", func(t *testing.T) {
		t.Parallel()

		mockClient := docker.NewMockAPIClient(t)
		ctx := context.Background()
		registry := container.Summary{}

		mockClient.EXPECT().
			VolumeRemove(ctx, "explicit-volume", false).
			Return(errors.New("removal failed")).
			Once()

		err := docker.CleanupRegistryVolume(
			ctx,
			mockClient,
			registry,
			"explicit-volume",
			"fallback",
			true,
		)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to remove registry volume")
	})
}

func TestCleanupOrphanedRegistryVolume(t *testing.T) {
	t.Parallel()

	t.Run("removes explicit volume first", func(t *testing.T) {
		t.Parallel()

		mockClient := docker.NewMockAPIClient(t)
		ctx := context.Background()

		mockClient.EXPECT().
			VolumeRemove(ctx, "explicit-volume", false).
			Return(nil).
			Once()

		err := docker.CleanupOrphanedRegistryVolume(ctx, mockClient, "explicit-volume", "fallback")

		require.NoError(t, err)
	})

	t.Run("falls back to normalized name when explicit not found", func(t *testing.T) {
		t.Parallel()

		mockClient := docker.NewMockAPIClient(t)
		ctx := context.Background()

		mockClient.EXPECT().
			VolumeRemove(ctx, "explicit-volume", false).
			Return(testNotFoundError{}).
			Once()

		mockClient.EXPECT().
			VolumeRemove(ctx, "docker.io", false).
			Return(nil).
			Once()

		err := docker.CleanupOrphanedRegistryVolume(
			ctx,
			mockClient,
			"explicit-volume",
			"kind-docker.io",
		)

		require.NoError(t, err)
	})

	t.Run("tries original fallback as last resort", func(t *testing.T) {
		t.Parallel()

		mockClient := docker.NewMockAPIClient(t)
		ctx := context.Background()

		mockClient.EXPECT().
			VolumeRemove(ctx, "docker.io", false).
			Return(testNotFoundError{}).
			Once()

		mockClient.EXPECT().
			VolumeRemove(ctx, "kind-docker.io", false).
			Return(nil).
			Once()

		err := docker.CleanupOrphanedRegistryVolume(ctx, mockClient, "", "kind-docker.io")

		require.NoError(t, err)
	})

	t.Run("returns nil when no volumes found", func(t *testing.T) {
		t.Parallel()

		mockClient := docker.NewMockAPIClient(t)
		ctx := context.Background()

		mockClient.EXPECT().
			VolumeRemove(ctx, "docker.io", false).
			Return(testNotFoundError{}).
			Once()

		mockClient.EXPECT().
			VolumeRemove(ctx, "kind-docker.io", false).
			Return(testNotFoundError{}).
			Once()

		err := docker.CleanupOrphanedRegistryVolume(ctx, mockClient, "", "kind-docker.io")

		require.NoError(t, err)
	})

	t.Run("returns error on unexpected failure", func(t *testing.T) {
		t.Parallel()

		mockClient := docker.NewMockAPIClient(t)
		ctx := context.Background()

		mockClient.EXPECT().
			VolumeRemove(ctx, "explicit-volume", false).
			Return(errors.New("unexpected error")).
			Once()

		err := docker.CleanupOrphanedRegistryVolume(ctx, mockClient, "explicit-volume", "fallback")

		require.Error(t, err)
	})
}

func TestRemoveRegistryVolume(t *testing.T) {
	t.Parallel()

	t.Run("returns false for empty volume name", func(t *testing.T) {
		t.Parallel()

		mockClient := docker.NewMockAPIClient(t)
		ctx := context.Background()

		removed, err := docker.RemoveRegistryVolume(ctx, mockClient, "")

		require.NoError(t, err)
		assert.False(t, removed)
		mockClient.AssertNotCalled(t, "VolumeRemove")
	})

	t.Run("returns false for whitespace only volume name", func(t *testing.T) {
		t.Parallel()

		mockClient := docker.NewMockAPIClient(t)
		ctx := context.Background()

		removed, err := docker.RemoveRegistryVolume(ctx, mockClient, "   ")

		require.NoError(t, err)
		assert.False(t, removed)
		mockClient.AssertNotCalled(t, "VolumeRemove")
	})

	t.Run("returns true when volume removed successfully", func(t *testing.T) {
		t.Parallel()

		mockClient := docker.NewMockAPIClient(t)
		ctx := context.Background()

		mockClient.EXPECT().
			VolumeRemove(ctx, "test-volume", false).
			Return(nil).
			Once()

		removed, err := docker.RemoveRegistryVolume(ctx, mockClient, "test-volume")

		require.NoError(t, err)
		assert.True(t, removed)
	})

	t.Run("trims whitespace from volume name", func(t *testing.T) {
		t.Parallel()

		mockClient := docker.NewMockAPIClient(t)
		ctx := context.Background()

		mockClient.EXPECT().
			VolumeRemove(ctx, "test-volume", false).
			Return(nil).
			Once()

		removed, err := docker.RemoveRegistryVolume(ctx, mockClient, "  test-volume  ")

		require.NoError(t, err)
		assert.True(t, removed)
	})

	t.Run("returns false when volume not found", func(t *testing.T) {
		t.Parallel()

		mockClient := docker.NewMockAPIClient(t)
		ctx := context.Background()

		mockClient.EXPECT().
			VolumeRemove(ctx, "missing-volume", false).
			Return(testNotFoundError{}).
			Once()

		removed, err := docker.RemoveRegistryVolume(ctx, mockClient, "missing-volume")

		require.NoError(t, err)
		assert.False(t, removed)
	})

	t.Run("returns error on unexpected failure", func(t *testing.T) {
		t.Parallel()

		mockClient := docker.NewMockAPIClient(t)
		ctx := context.Background()

		mockClient.EXPECT().
			VolumeRemove(ctx, "test-volume", false).
			Return(errors.New("permission denied")).
			Once()

		removed, err := docker.RemoveRegistryVolume(ctx, mockClient, "test-volume")

		require.Error(t, err)
		assert.False(t, removed)
		assert.Contains(t, err.Error(), "failed to remove registry volume")
	})
}
