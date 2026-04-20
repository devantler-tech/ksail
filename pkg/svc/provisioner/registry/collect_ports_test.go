package registry_test

import (
	"context"
	"errors"
	"testing"

	dockerclient "github.com/devantler-tech/ksail/v7/pkg/client/docker"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/registry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

var errPortLookup = errors.New("port lookup failed")

// --- CollectExistingRegistryPorts ---

func TestCollectExistingRegistryPorts_NilBackend_ReturnsEmpty(t *testing.T) {
	t.Parallel()

	ports, err := registry.CollectExistingRegistryPorts(context.Background(), nil)
	require.NoError(t, err)
	assert.Empty(t, ports)
}

func TestCollectExistingRegistryPorts_NoRegistries_ReturnsEmpty(t *testing.T) {
	t.Parallel()

	mockBackend := registry.NewMockBackend(t)
	mockBackend.EXPECT().ListRegistries(mock.Anything).Return([]string{}, nil).Once()

	ports, err := registry.CollectExistingRegistryPorts(context.Background(), mockBackend)
	require.NoError(t, err)
	assert.Empty(t, ports)
}

func TestCollectExistingRegistryPorts_CollectsExistingPorts(t *testing.T) {
	t.Parallel()

	mockBackend := registry.NewMockBackend(t)
	mockBackend.EXPECT().
		ListRegistries(mock.Anything).
		Return([]string{"mirror-1", "mirror-2"}, nil).
		Once()
	mockBackend.EXPECT().GetRegistryPort(mock.Anything, "mirror-1").Return(5000, nil).Once()
	mockBackend.EXPECT().GetRegistryPort(mock.Anything, "mirror-2").Return(5001, nil).Once()

	ports, err := registry.CollectExistingRegistryPorts(context.Background(), mockBackend)
	require.NoError(t, err)
	assert.Len(t, ports, 2)

	_, has5000 := ports[5000]
	assert.True(t, has5000)

	_, has5001 := ports[5001]
	assert.True(t, has5001)
}

func TestCollectExistingRegistryPorts_SkipsNotFoundErrors(t *testing.T) {
	t.Parallel()

	mockBackend := registry.NewMockBackend(t)
	mockBackend.EXPECT().ListRegistries(mock.Anything).Return([]string{"ghost"}, nil).Once()
	mockBackend.EXPECT().GetRegistryPort(mock.Anything, "ghost").
		Return(0, dockerclient.ErrRegistryNotFound).Once()

	ports, err := registry.CollectExistingRegistryPorts(context.Background(), mockBackend)
	require.NoError(t, err)
	assert.Empty(t, ports)
}

func TestCollectExistingRegistryPorts_SkipsPortNotFoundErrors(t *testing.T) {
	t.Parallel()

	mockBackend := registry.NewMockBackend(t)
	mockBackend.EXPECT().ListRegistries(mock.Anything).Return([]string{"no-port"}, nil).Once()
	mockBackend.EXPECT().GetRegistryPort(mock.Anything, "no-port").
		Return(0, dockerclient.ErrRegistryPortNotFound).Once()

	ports, err := registry.CollectExistingRegistryPorts(context.Background(), mockBackend)
	require.NoError(t, err)
	assert.Empty(t, ports)
}

func TestCollectExistingRegistryPorts_PropagatesOtherErrors(t *testing.T) {
	t.Parallel()

	mockBackend := registry.NewMockBackend(t)
	mockBackend.EXPECT().ListRegistries(mock.Anything).Return([]string{"broken"}, nil).Once()
	mockBackend.EXPECT().GetRegistryPort(mock.Anything, "broken").
		Return(0, errPortLookup).Once()

	_, err := registry.CollectExistingRegistryPorts(context.Background(), mockBackend)
	require.Error(t, err)
	assert.ErrorContains(t, err, "broken")
}

func TestCollectExistingRegistryPorts_ListFails(t *testing.T) {
	t.Parallel()

	mockBackend := registry.NewMockBackend(t)
	mockBackend.EXPECT().ListRegistries(mock.Anything).
		Return(nil, errPortLookup).Once()

	_, err := registry.CollectExistingRegistryPorts(context.Background(), mockBackend)
	require.Error(t, err)
	assert.ErrorContains(t, err, "failed to list existing registries")
}

func TestCollectExistingRegistryPorts_SkipsEmptyNames(t *testing.T) {
	t.Parallel()

	mockBackend := registry.NewMockBackend(t)
	mockBackend.EXPECT().ListRegistries(mock.Anything).
		Return([]string{"", "  ", "valid-reg"}, nil).Once()
	mockBackend.EXPECT().GetRegistryPort(mock.Anything, "valid-reg").
		Return(5000, nil).Once()

	ports, err := registry.CollectExistingRegistryPorts(context.Background(), mockBackend)
	require.NoError(t, err)
	assert.Len(t, ports, 1)

	_, has5000 := ports[5000]
	assert.True(t, has5000)
}

func TestCollectExistingRegistryPorts_SkipsZeroPorts(t *testing.T) {
	t.Parallel()

	mockBackend := registry.NewMockBackend(t)
	mockBackend.EXPECT().ListRegistries(mock.Anything).
		Return([]string{"zero-port"}, nil).Once()
	mockBackend.EXPECT().GetRegistryPort(mock.Anything, "zero-port").
		Return(0, nil).Once()

	ports, err := registry.CollectExistingRegistryPorts(context.Background(), mockBackend)
	require.NoError(t, err)
	assert.Empty(t, ports)
}
