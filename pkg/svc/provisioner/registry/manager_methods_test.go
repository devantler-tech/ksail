package registry_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/client/docker"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

var errBackendFailure = errors.New("backend failure")

// --- Manager.EnsureBatch ---

func TestManager_EnsureBatch_Success(t *testing.T) {
	t.Parallel()

	mockBackend := registry.NewMockBackend(t)

	// ListRegistries is called during newMirrorBatch
	mockBackend.EXPECT().ListRegistries(mock.Anything).Return([]string{}, nil).Once()

	// CreateRegistry is called for each registry
	mockBackend.EXPECT().CreateRegistry(mock.Anything, mock.Anything).Return(nil).Times(2)

	mgr, err := registry.NewManager(mockBackend)
	require.NoError(t, err)

	registries := []registry.Info{
		{Host: "docker.io", Name: "docker-io", Upstream: "https://registry-1.docker.io"},
		{Host: "ghcr.io", Name: "ghcr-io", Upstream: "https://ghcr.io"},
	}

	var buf bytes.Buffer

	err = mgr.EnsureBatch(context.Background(), registries, "test-cluster", "test-net", &buf)
	require.NoError(t, err)
}

func TestManager_EnsureBatch_EmptyRegistries(t *testing.T) {
	t.Parallel()

	mockBackend := registry.NewMockBackend(t)

	mgr, err := registry.NewManager(mockBackend)
	require.NoError(t, err)

	var buf bytes.Buffer

	err = mgr.EnsureBatch(context.Background(), nil, "test-cluster", "test-net", &buf)
	require.NoError(t, err)
}

func TestManager_EnsureBatch_CreateFailsRollsBack(t *testing.T) {
	t.Parallel()

	mockBackend := registry.NewMockBackend(t)

	// ListRegistries for newMirrorBatch
	mockBackend.EXPECT().ListRegistries(mock.Anything).Return([]string{}, nil).Once()

	// First CreateRegistry succeeds
	mockBackend.EXPECT().
		CreateRegistry(mock.Anything, mock.MatchedBy(func(c docker.RegistryConfig) bool {
			return c.Name == "mirror-1"
		})).
		Return(nil).
		Once()

	// Second CreateRegistry fails
	mockBackend.EXPECT().
		CreateRegistry(mock.Anything, mock.MatchedBy(func(c docker.RegistryConfig) bool {
			return c.Name == "mirror-2"
		})).
		Return(errBackendFailure).
		Once()

	// Rollback: DeleteRegistry called for the first successfully created one
	mockBackend.EXPECT().DeleteRegistry(
		mock.Anything, "mirror-1", "cluster", false, "", "",
	).Return(nil).Once()

	mgr, err := registry.NewManager(mockBackend)
	require.NoError(t, err)

	registries := []registry.Info{
		{Host: "docker.io", Name: "mirror-1", Upstream: "https://registry-1.docker.io"},
		{Host: "ghcr.io", Name: "mirror-2", Upstream: "https://ghcr.io"},
	}

	var buf bytes.Buffer

	err = mgr.EnsureBatch(context.Background(), registries, "cluster", "", &buf)
	require.Error(t, err)
	assert.ErrorContains(t, err, "mirror-2")
}

// --- Manager.EnsureOne ---

func TestManager_EnsureOne_CreatesNew(t *testing.T) {
	t.Parallel()

	mockBackend := registry.NewMockBackend(t)

	// ListRegistries for newMirrorBatch
	mockBackend.EXPECT().ListRegistries(mock.Anything).Return([]string{}, nil).Once()

	// CreateRegistry
	mockBackend.EXPECT().CreateRegistry(mock.Anything, mock.Anything).Return(nil).Once()

	mgr, err := registry.NewManager(mockBackend)
	require.NoError(t, err)

	spec := registry.Info{
		Host:     "docker.io",
		Name:     "mirror-docker",
		Upstream: "https://registry-1.docker.io",
	}

	var buf bytes.Buffer

	created, err := mgr.EnsureOne(context.Background(), spec, "cluster", &buf)
	require.NoError(t, err)
	assert.True(t, created)
}

func TestManager_EnsureOne_SkipsExisting(t *testing.T) {
	t.Parallel()

	mockBackend := registry.NewMockBackend(t)

	// ListRegistries returns the existing registry
	mockBackend.EXPECT().ListRegistries(mock.Anything).Return([]string{"mirror-docker"}, nil).Once()

	// CreateRegistry still called (idempotent create)
	mockBackend.EXPECT().CreateRegistry(mock.Anything, mock.Anything).Return(nil).Once()

	mgr, err := registry.NewManager(mockBackend)
	require.NoError(t, err)

	spec := registry.Info{
		Host:     "docker.io",
		Name:     "mirror-docker",
		Upstream: "https://registry-1.docker.io",
	}

	var buf bytes.Buffer

	created, err := mgr.EnsureOne(context.Background(), spec, "cluster", &buf)
	require.NoError(t, err)
	assert.False(t, created)
}

func TestManager_EnsureOne_CreateFails_RollsBack(t *testing.T) {
	t.Parallel()

	mockBackend := registry.NewMockBackend(t)

	mockBackend.EXPECT().ListRegistries(mock.Anything).Return([]string{}, nil).Once()
	mockBackend.EXPECT().
		CreateRegistry(mock.Anything, mock.Anything).
		Return(errBackendFailure).
		Once()

	// No rollback expected since nothing was created (create failed)

	mgr, err := registry.NewManager(mockBackend)
	require.NoError(t, err)

	spec := registry.Info{
		Host:     "docker.io",
		Name:     "mirror-docker",
		Upstream: "https://registry-1.docker.io",
	}

	var buf bytes.Buffer

	_, err = mgr.EnsureOne(context.Background(), spec, "cluster", &buf)
	require.Error(t, err)
	assert.ErrorContains(t, err, "mirror-docker")
}

func TestManager_EnsureOne_ListFails(t *testing.T) {
	t.Parallel()

	mockBackend := registry.NewMockBackend(t)

	mockBackend.EXPECT().ListRegistries(mock.Anything).Return(nil, errBackendFailure).Once()

	mgr, err := registry.NewManager(mockBackend)
	require.NoError(t, err)

	spec := registry.Info{
		Host: "docker.io",
		Name: "mirror-docker",
	}

	var buf bytes.Buffer

	_, err = mgr.EnsureOne(context.Background(), spec, "cluster", &buf)
	require.Error(t, err)
	assert.ErrorContains(t, err, "create registry tracker")
}

// --- Manager.Cleanup ---

func TestManager_Cleanup_Success(t *testing.T) {
	t.Parallel()

	mockBackend := registry.NewMockBackend(t)

	mockBackend.EXPECT().DeleteRegistry(
		mock.Anything, "mirror-1", "cluster", true, "net", "vol-1",
	).Return(nil).Once()

	mgr, err := registry.NewManager(mockBackend)
	require.NoError(t, err)

	registries := []registry.Info{
		{Name: "mirror-1", Volume: "vol-1"},
	}

	var buf bytes.Buffer

	err = mgr.Cleanup(context.Background(), registries, "cluster", true, "net", &buf)
	require.NoError(t, err)
}

func TestManager_Cleanup_EmptyRegistries(t *testing.T) {
	t.Parallel()

	mockBackend := registry.NewMockBackend(t)

	mgr, err := registry.NewManager(mockBackend)
	require.NoError(t, err)

	var buf bytes.Buffer

	err = mgr.Cleanup(context.Background(), nil, "cluster", true, "net", &buf)
	require.NoError(t, err)
}

// --- Manager.CleanupOne ---

func TestManager_CleanupOne_Success(t *testing.T) {
	t.Parallel()

	mockBackend := registry.NewMockBackend(t)

	mockBackend.EXPECT().DeleteRegistry(
		mock.Anything, "mirror-1", "cluster", true, "net", "vol-1",
	).Return(nil).Once()

	mgr, err := registry.NewManager(mockBackend)
	require.NoError(t, err)

	err = mgr.CleanupOne(
		context.Background(),
		registry.Info{Name: "mirror-1", Volume: "vol-1"},
		"cluster",
		true,
		"net",
	)
	require.NoError(t, err)
}

func TestManager_CleanupOne_DeleteFails(t *testing.T) {
	t.Parallel()

	mockBackend := registry.NewMockBackend(t)

	mockBackend.EXPECT().DeleteRegistry(
		mock.Anything, "mirror-1", "cluster", false, "net", "",
	).Return(errBackendFailure).Once()

	mgr, err := registry.NewManager(mockBackend)
	require.NoError(t, err)

	err = mgr.CleanupOne(
		context.Background(),
		registry.Info{Name: "mirror-1"},
		"cluster",
		false,
		"net",
	)
	require.Error(t, err)
	assert.ErrorContains(t, err, "delete registry mirror-1")
}
