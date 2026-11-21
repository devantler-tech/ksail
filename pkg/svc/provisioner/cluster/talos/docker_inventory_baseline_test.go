package talosprovisioner_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/client/docker"
	talosprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/talos"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

var errDockerInventoryTestEndpointUnavailable = errors.New("fixture endpoint unavailable")

func TestCurrentConfigDoesNotOmitDockerCapacity(t *testing.T) {
	t.Parallel()

	for _, workerAddress := range []string{"", "192.0.2.3"} {
		t.Run("worker address "+workerAddress, func(t *testing.T) {
			t.Parallel()

			client := docker.NewMockAPIClient(t)
			client.EXPECT().ContainerList(mock.Anything, mock.Anything).
				Run(func(_ context.Context, options container.ListOptions) {
					assert.True(t, options.All, "stopped nodes still consume capacity")
				}).
				Return([]container.Summary{
					dockerInventoryNode("controlplane-1", "192.0.2.2"),
					dockerInventoryNode("worker-1", workerAddress),
				}, nil)
			client.EXPECT().ContainerInspect(mock.Anything, mock.Anything).
				Return(container.InspectResponse{}, errDockerInventoryTestEndpointUnavailable).
				Maybe()

			provisioner := talosprovisioner.NewProvisioner(createTestTalosConfigs(t, "inventory"), nil).
				WithDockerClient(client)

			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
			defer cancel()

			spec, providerSpec, err := provisioner.GetCurrentConfig(ctx, "inventory")
			if workerAddress == "" {
				require.Error(t, err, "an unaddressed node must not disappear from the baseline")
				assert.Nil(t, spec)
				assert.Nil(t, providerSpec)

				return
			}

			require.NoError(t, err)
			require.NotNil(t, spec)
			assert.Equal(t, int32(1), spec.ControlPlanes)
			assert.Equal(t, int32(1), spec.Workers)
		})
	}
}

func dockerInventoryNode(name, address string) container.Summary {
	return container.Summary{
		ID: name, Names: []string{"/inventory-" + name},
		NetworkSettings: &container.NetworkSettingsSummary{
			Networks: map[string]*network.EndpointSettings{
				"inventory": {IPAddress: address},
			},
		},
	}
}
