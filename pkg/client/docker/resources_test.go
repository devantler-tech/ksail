package docker_test

import (
	"testing"

	docker "github.com/devantler-tech/ksail/v6/pkg/client/docker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

//nolint:paralleltest // Shares process-wide Docker env state with TestGetConcreteDockerClient_InvalidEnv.
func TestGetConcreteDockerClient(t *testing.T) {
	concreteClient, err := docker.GetConcreteDockerClient()
	if err != nil {
		t.Skipf("Docker unavailable: %v", err)
	}

	require.NotNil(t, concreteClient)
}

func TestGetConcreteDockerClient_InvalidEnv(t *testing.T) {
	t.Setenv("DOCKER_HOST", "://")
	t.Setenv("DOCKER_TLS_VERIFY", "")
	t.Setenv("DOCKER_CERT_PATH", "")

	concreteClient, err := docker.GetConcreteDockerClient()

	require.Error(t, err)
	assert.Nil(t, concreteClient)
}

func TestResources_Close(t *testing.T) {
	t.Parallel()

	t.Run("close with nil client does not panic", func(t *testing.T) {
		t.Parallel()

		resources := &docker.Resources{
			Client:          nil,
			RegistryManager: nil,
		}

		assert.NotPanics(t, func() {
			resources.Close()
		})
	})

	t.Run("close with mock client does not panic", func(t *testing.T) {
		t.Parallel()

		mockClient := docker.NewMockAPIClient(t)
		mockClient.EXPECT().
			Close().
			Return(nil).
			Once()

		resources := &docker.Resources{
			Client: mockClient,
		}

		assert.NotPanics(t, func() {
			resources.Close()
		})
	})
}
