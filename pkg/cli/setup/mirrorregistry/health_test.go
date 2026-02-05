package mirrorregistry_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/cli/setup/mirrorregistry"
	"github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/registry"
	"github.com/docker/docker/client"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestWaitForRegistriesReady_EmptyRegistries(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	err := mirrorregistry.WaitForRegistriesReady(
		context.Background(),
		nil,
		nil,
		&buf,
	)

	require.NoError(t, err)
	assert.Empty(t, buf.String(), "should not write any messages for empty registries")
}

func TestWaitForRegistriesReady_BackendFactoryError(t *testing.T) {
	t.Parallel()

	factoryErr := errors.New("docker unavailable")
	cleanup := registry.SetBackendFactoryForTests(
		func(_ client.APIClient) (registry.Backend, error) {
			return nil, factoryErr
		},
	)
	defer cleanup()

	var buf bytes.Buffer

	infos := []registry.Info{
		{Name: "mirror-docker-io", Host: "docker.io", Port: 5000},
	}

	err := mirrorregistry.WaitForRegistriesReady(
		context.Background(),
		nil,
		infos,
		&buf,
	)

	require.Error(t, err)
	assert.ErrorIs(t, err, factoryErr)
}

func TestWaitForRegistriesReady_Success(t *testing.T) {
	t.Parallel()

	mockBackend := registry.NewMockBackend(t)
	mockBackend.On("WaitForRegistriesReady", mock.Anything, mock.MatchedBy(func(m map[string]string) bool {
		_, ok := m["mirror-docker-io"]
		return ok && len(m) == 1
	})).Return(nil)

	cleanup := registry.SetBackendFactoryForTests(
		func(_ client.APIClient) (registry.Backend, error) {
			return mockBackend, nil
		},
	)
	defer cleanup()

	var buf bytes.Buffer

	infos := []registry.Info{
		{Name: "mirror-docker-io", Host: "docker.io", Port: 5000},
	}

	err := mirrorregistry.WaitForRegistriesReady(
		context.Background(),
		nil,
		infos,
		&buf,
	)

	require.NoError(t, err)
	mockBackend.AssertExpectations(t)
}

func TestWaitForRegistriesReady_BackendError(t *testing.T) {
	t.Parallel()

	backendErr := errors.New("registry timeout")
	mockBackend := registry.NewMockBackend(t)
	mockBackend.On("WaitForRegistriesReady", mock.Anything, mock.Anything).Return(backendErr)

	cleanup := registry.SetBackendFactoryForTests(
		func(_ client.APIClient) (registry.Backend, error) {
			return mockBackend, nil
		},
	)
	defer cleanup()

	var buf bytes.Buffer

	infos := []registry.Info{
		{Name: "mirror-docker-io", Host: "docker.io", Port: 5000},
		{Name: "mirror-ghcr-io", Host: "ghcr.io", Port: 5001},
	}

	err := mirrorregistry.WaitForRegistriesReady(
		context.Background(),
		nil,
		infos,
		&buf,
	)

	require.Error(t, err)
	assert.ErrorIs(t, err, backendErr)
	mockBackend.AssertExpectations(t)
}

func TestWaitForRegistriesReady_MultipleRegistries(t *testing.T) {
	t.Parallel()

	mockBackend := registry.NewMockBackend(t)
	mockBackend.On("WaitForRegistriesReady", mock.Anything, mock.MatchedBy(func(m map[string]string) bool {
		_, hasDockerhub := m["mirror-docker-io"]
		_, hasGhcr := m["mirror-ghcr-io"]
		return hasDockerhub && hasGhcr && len(m) == 2
	})).Return(nil)

	cleanup := registry.SetBackendFactoryForTests(
		func(_ client.APIClient) (registry.Backend, error) {
			return mockBackend, nil
		},
	)
	defer cleanup()

	var buf bytes.Buffer

	infos := []registry.Info{
		{Name: "mirror-docker-io", Host: "docker.io", Port: 5000},
		{Name: "mirror-ghcr-io", Host: "ghcr.io", Port: 5001},
	}

	err := mirrorregistry.WaitForRegistriesReady(
		context.Background(),
		nil,
		infos,
		&buf,
	)

	require.NoError(t, err)
	mockBackend.AssertExpectations(t)
}
