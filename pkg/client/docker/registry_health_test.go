//nolint:err113 // Tests use dynamic errors for mock behaviors.
package docker_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	docker "github.com/devantler-tech/ksail/v7/pkg/client/docker"
	"github.com/docker/docker/api/types/container"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestWaitForRegistryReady_MirrorRegistryNoPort(t *testing.T) {
	t.Parallel()

	// When registry has no host port (mirror), it should just check if running
	mockClient, manager, ctx := setupTestRegistryManager(t)

	// IsRegistryInUse → list registry containers (running)
	mockClient.EXPECT().
		ContainerList(ctx, mock.Anything).
		Return([]container.Summary{
			{
				ID:     "mirror-id",
				Names:  []string{"/docker-mirror"},
				Labels: map[string]string{docker.RegistryLabelKey: "docker-mirror"},
				State:  "running",
			},
		}, nil).
		Once()

	// GetRegistryPort → list containers (no matching port)
	mockClient.EXPECT().
		ContainerList(ctx, mock.Anything).
		Return([]container.Summary{
			{
				ID:     "mirror-id",
				Names:  []string{"/docker-mirror"},
				Labels: map[string]string{docker.RegistryLabelKey: "docker-mirror"},
				// No ports → mirror registry
			},
		}, nil).
		Once()

	// IsRegistryInUse again to verify running
	mockClient.EXPECT().
		ContainerList(ctx, mock.Anything).
		Return([]container.Summary{
			{
				ID:     "mirror-id",
				Names:  []string{"/docker-mirror"},
				Labels: map[string]string{docker.RegistryLabelKey: "docker-mirror"},
				State:  "running",
			},
		}, nil).
		Once()

	err := manager.WaitForRegistryReady(ctx, "docker-mirror", "")

	require.NoError(t, err)
}

func TestWaitForRegistryReady_NotRunning(t *testing.T) {
	t.Parallel()

	mockClient, manager, ctx := setupTestRegistryManager(t)

	// IsRegistryInUse → list (not running)
	mockClient.EXPECT().
		ContainerList(ctx, mock.Anything).
		Return([]container.Summary{
			{
				ID:     "reg-id",
				Names:  []string{"/docker.io"},
				Labels: map[string]string{docker.RegistryLabelKey: "docker.io"},
				State:  "exited",
			},
		}, nil).
		Once()

	err := manager.WaitForRegistryReady(ctx, "docker.io", "")

	require.Error(t, err)
	assert.ErrorIs(t, err, docker.ErrRegistryNotFound)
}

func TestWaitForRegistryReady_ListError(t *testing.T) {
	t.Parallel()

	mockClient, manager, ctx := setupTestRegistryManager(t)

	mockClient.EXPECT().
		ContainerList(ctx, mock.Anything).
		Return(nil, errors.New("docker error")).
		Once()

	err := manager.WaitForRegistryReady(ctx, "docker.io", "")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to check if registry")
}

//nolint:dupl // Duplicated setup keeps the parallel test cases readable.
func TestWaitForRegistryReadyWithTimeout_Success(t *testing.T) {
	t.Parallel()

	// Create a local HTTP server that responds to /v2/
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	// Parse port from test server
	parts := strings.Split(server.Listener.Addr().String(), ":")
	port, _ := strconv.Atoi(parts[len(parts)-1])

	mockClient, manager, ctx := setupTestRegistryManager(t)

	// IsRegistryInUse → running
	mockClient.EXPECT().
		ContainerList(ctx, mock.Anything).
		Return([]container.Summary{
			{
				ID:     "reg-id",
				Names:  []string{"/test-registry"},
				Labels: map[string]string{docker.RegistryLabelKey: "test-registry"},
				State:  "running",
			},
		}, nil).
		Once()

	// GetRegistryPort → returns the test server port
	mockClient.EXPECT().
		ContainerList(ctx, mock.Anything).
		Return([]container.Summary{
			{
				ID:     "reg-id",
				Names:  []string{"/test-registry"},
				Labels: map[string]string{docker.RegistryLabelKey: "test-registry"},
				Ports: []container.Port{
					{
						PrivatePort: docker.DefaultRegistryPort,
						PublicPort:  uint16(port), //nolint:gosec // test port
					},
				},
			},
		}, nil).
		Once()

	err := manager.WaitForRegistryReadyWithTimeout(ctx, "test-registry", 5*time.Second)

	require.NoError(t, err)
}

func TestWaitForRegistryReadyWithTimeout_ContextCancelled(t *testing.T) {
	t.Parallel()

	// Server that delays response so context gets cancelled first
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
			return
		case <-time.After(10 * time.Second):
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	defer server.Close()

	parts := strings.Split(server.Listener.Addr().String(), ":")
	port, _ := strconv.Atoi(parts[len(parts)-1])

	mockClient := docker.NewMockAPIClient(t)
	manager, err := docker.NewRegistryManager(mockClient)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	registryContainers := []container.Summary{
		{
			ID:     "reg-id",
			Names:  []string{"/test-registry"},
			Labels: map[string]string{docker.RegistryLabelKey: "test-registry"},
			State:  "running",
			Ports: []container.Port{
				{
					PrivatePort: docker.DefaultRegistryPort,
					PublicPort:  uint16(port), //nolint:gosec // test port
				},
			},
		},
	}

	// Use Maybe() since we can't predict exact call count
	mockClient.EXPECT().
		ContainerList(mock.Anything, mock.Anything).
		Return(registryContainers, nil).
		Maybe()

	// Cancel quickly
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	waitErr := manager.WaitForRegistryReadyWithTimeout(ctx, "test-registry", 30*time.Second)

	require.Error(t, waitErr)
	assert.ErrorIs(t, waitErr, docker.ErrRegistryHealthCheckCancelled)
}

func TestWaitForRegistryReadyWithTimeout_Timeout(t *testing.T) {
	t.Parallel()

	// Server that returns 500
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	parts := strings.Split(server.Listener.Addr().String(), ":")
	port, _ := strconv.Atoi(parts[len(parts)-1])

	mockClient, manager, ctx := setupTestRegistryManager(t)

	// IsRegistryInUse
	mockClient.EXPECT().
		ContainerList(ctx, mock.Anything).
		Return([]container.Summary{
			{
				ID:     "reg-id",
				Names:  []string{"/test-registry"},
				Labels: map[string]string{docker.RegistryLabelKey: "test-registry"},
				State:  "running",
			},
		}, nil).
		Once()

	// GetRegistryPort
	mockClient.EXPECT().
		ContainerList(ctx, mock.Anything).
		Return([]container.Summary{
			{
				ID:     "reg-id",
				Names:  []string{"/test-registry"},
				Labels: map[string]string{docker.RegistryLabelKey: "test-registry"},
				Ports: []container.Port{
					{
						PrivatePort: docker.DefaultRegistryPort,
						PublicPort:  uint16(port), //nolint:gosec // test port
					},
				},
			},
		}, nil).
		Once()

	// Use very short timeout
	waitErr := manager.WaitForRegistryReadyWithTimeout(ctx, "test-registry", 1*time.Second)

	require.Error(t, waitErr)
	assert.ErrorIs(t, waitErr, docker.ErrRegistryNotReady)
}

//nolint:dupl // Duplicated setup keeps the parallel test cases readable.
func TestWaitForRegistryReadyWithTimeout_401IsReady(t *testing.T) {
	t.Parallel()

	// Server that returns 401 (auth required) - still considered "ready"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()

	parts := strings.Split(server.Listener.Addr().String(), ":")
	port, _ := strconv.Atoi(parts[len(parts)-1])

	mockClient, manager, ctx := setupTestRegistryManager(t)

	// IsRegistryInUse
	mockClient.EXPECT().
		ContainerList(ctx, mock.Anything).
		Return([]container.Summary{
			{
				ID:     "reg-id",
				Names:  []string{"/test-registry"},
				Labels: map[string]string{docker.RegistryLabelKey: "test-registry"},
				State:  "running",
			},
		}, nil).
		Once()

	// GetRegistryPort
	mockClient.EXPECT().
		ContainerList(ctx, mock.Anything).
		Return([]container.Summary{
			{
				ID:     "reg-id",
				Names:  []string{"/test-registry"},
				Labels: map[string]string{docker.RegistryLabelKey: "test-registry"},
				Ports: []container.Port{
					{
						PrivatePort: docker.DefaultRegistryPort,
						PublicPort:  uint16(port), //nolint:gosec // test port
					},
				},
			},
		}, nil).
		Once()

	err := manager.WaitForRegistryReadyWithTimeout(ctx, "test-registry", 5*time.Second)

	require.NoError(t, err)
}

func TestWaitForRegistriesReady(t *testing.T) {
	t.Parallel()

	t.Run("returns nil for empty map", func(t *testing.T) {
		t.Parallel()

		_, manager, ctx := setupTestRegistryManager(t)

		err := manager.WaitForRegistriesReady(ctx, map[string]string{})

		require.NoError(t, err)
	})

	t.Run("returns error when a registry fails", func(t *testing.T) {
		t.Parallel()

		mockClient, manager, ctx := setupTestRegistryManager(t)

		// First registry check fails
		mockClient.EXPECT().
			ContainerList(ctx, mock.Anything).
			Return(nil, errors.New("docker error")).
			Once()

		err := manager.WaitForRegistriesReady(ctx, map[string]string{
			"docker.io": "",
		})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed health check")
	})
}

func TestWaitForContainerRegistryReady_NotRunning(t *testing.T) {
	t.Parallel()

	mockClient, manager, ctx := setupTestRegistryManager(t)

	// IsContainerRunning → not running
	mockClient.EXPECT().
		ContainerList(ctx, mock.Anything).
		Return([]container.Summary{
			{
				ID:    "c1",
				Names: []string{"/k3d-registry"},
				State: "exited",
			},
		}, nil).
		Once()

	err := manager.WaitForContainerRegistryReady(ctx, "k3d-registry", 5*time.Second)

	require.Error(t, err)
	assert.ErrorIs(t, err, docker.ErrRegistryNotFound)
}

func TestWaitForContainerRegistryReady_NoContainer(t *testing.T) {
	t.Parallel()

	mockClient, manager, ctx := setupTestRegistryManager(t)

	mockClient.EXPECT().
		ContainerList(ctx, mock.Anything).
		Return([]container.Summary{}, nil).
		Once()

	err := manager.WaitForContainerRegistryReady(ctx, "k3d-registry", 5*time.Second)

	require.Error(t, err)
	assert.ErrorIs(t, err, docker.ErrRegistryNotFound)
}

func TestWaitForContainerRegistryReady_Success(t *testing.T) {
	t.Parallel()

	// Create a local HTTP server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	parts := strings.Split(server.Listener.Addr().String(), ":")
	port, _ := strconv.Atoi(parts[len(parts)-1])

	mockClient, manager, ctx := setupTestRegistryManager(t)

	// IsContainerRunning → running
	mockClient.EXPECT().
		ContainerList(ctx, mock.Anything).
		Return([]container.Summary{
			{
				ID:    "c1",
				Names: []string{"/k3d-registry"},
				State: "running",
			},
		}, nil).
		Once()

	// GetContainerPort
	mockClient.EXPECT().
		ContainerList(ctx, mock.Anything).
		Return([]container.Summary{
			{
				ID:    "c1",
				Names: []string{"/k3d-registry"},
				Ports: []container.Port{
					{
						PrivatePort: docker.DefaultRegistryPort,
						PublicPort:  uint16(port), //nolint:gosec // test port
					},
				},
			},
		}, nil).
		Once()

	err := manager.WaitForContainerRegistryReady(ctx, "k3d-registry", 5*time.Second)

	require.NoError(t, err)
}

func TestWaitForMirrorRegistryNotRunning(t *testing.T) {
	t.Parallel()

	mockClient, manager, ctx := setupTestRegistryManager(t)

	// IsRegistryInUse → running
	mockClient.EXPECT().
		ContainerList(ctx, mock.Anything).
		Return([]container.Summary{
			{
				ID:     "mirror-id",
				Names:  []string{"/docker-mirror"},
				Labels: map[string]string{docker.RegistryLabelKey: "docker-mirror"},
				State:  "running",
			},
		}, nil).
		Once()

	// GetRegistryPort → no port (ErrRegistryPortNotFound)
	mockClient.EXPECT().
		ContainerList(ctx, mock.Anything).
		Return([]container.Summary{
			{
				ID:     "mirror-id",
				Names:  []string{"/docker-mirror"},
				Labels: map[string]string{docker.RegistryLabelKey: "docker-mirror"},
				// No ports → mirror
			},
		}, nil).
		Once()

	// IsRegistryInUse → not running anymore
	mockClient.EXPECT().
		ContainerList(ctx, mock.Anything).
		Return([]container.Summary{
			{
				ID:     "mirror-id",
				Names:  []string{"/docker-mirror"},
				Labels: map[string]string{docker.RegistryLabelKey: "docker-mirror"},
				State:  "exited",
			},
		}, nil).
		Once()

	err := manager.WaitForRegistryReadyWithTimeout(ctx, "docker-mirror", 5*time.Second)

	require.Error(t, err)
	assert.ErrorIs(t, err, docker.ErrRegistryNotFound)
}

func TestWaitForRegistriesReadyWithTimeout(t *testing.T) {
	t.Parallel()

	t.Run("returns nil for empty map", func(t *testing.T) {
		t.Parallel()

		_, manager, ctx := setupTestRegistryManager(t)

		err := manager.WaitForRegistriesReadyWithTimeout(ctx, map[string]string{}, 5*time.Second)

		require.NoError(t, err)
	})
}
