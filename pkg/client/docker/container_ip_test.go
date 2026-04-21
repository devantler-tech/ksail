package docker_test

import (
	"context"
	"errors"
	"testing"

	docker "github.com/devantler-tech/ksail/v7/pkg/client/docker"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

//nolint:funlen // Table-driven test coverage is naturally long.
func TestResolveContainerIPOnNetwork(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		containerName string
		networkName   string
		setupMock     func(*docker.MockAPIClient, context.Context)
		expectedIP    string
		expectedErr   error
		errContains   string
	}{
		{
			name:          "returns IP when container is connected to network",
			containerName: "my-registry",
			networkName:   "kind-test",
			setupMock: func(m *docker.MockAPIClient, ctx context.Context) {
				m.EXPECT().
					ContainerInspect(ctx, "my-registry").
					Return(container.InspectResponse{
						NetworkSettings: &container.NetworkSettings{
							Networks: map[string]*network.EndpointSettings{
								"kind-test": {IPAddress: "172.18.0.5"},
							},
						},
					}, nil).
					Once()
			},
			expectedIP: "172.18.0.5",
		},
		{
			name:          "returns error when inspect fails",
			containerName: "missing-container",
			networkName:   "kind-test",
			setupMock: func(m *docker.MockAPIClient, ctx context.Context) {
				m.EXPECT().
					ContainerInspect(ctx, "missing-container").
					//nolint:err113 // Tests use controlled mock errors.
					Return(container.InspectResponse{}, errors.New("no such container")).
					Once()
			},
			errContains: "inspect container missing-container",
		},
		{
			name:          "returns ErrNoNetworkSettings when NetworkSettings is nil",
			containerName: "my-registry",
			networkName:   "kind-test",
			setupMock: func(m *docker.MockAPIClient, ctx context.Context) {
				m.EXPECT().
					ContainerInspect(ctx, "my-registry").
					Return(container.InspectResponse{
						NetworkSettings: nil,
					}, nil).
					Once()
			},
			expectedErr: docker.ErrNoNetworkSettings,
		},
		{
			name:          "returns ErrNoNetworkSettings when Networks map is nil",
			containerName: "my-registry",
			networkName:   "kind-test",
			setupMock: func(m *docker.MockAPIClient, ctx context.Context) {
				m.EXPECT().
					ContainerInspect(ctx, "my-registry").
					Return(container.InspectResponse{
						NetworkSettings: &container.NetworkSettings{
							Networks: nil,
						},
					}, nil).
					Once()
			},
			expectedErr: docker.ErrNoNetworkSettings,
		},
		{
			name:          "returns ErrNotConnectedToNetwork when network not found",
			containerName: "my-registry",
			networkName:   "k3d-other",
			setupMock: func(m *docker.MockAPIClient, ctx context.Context) {
				m.EXPECT().
					ContainerInspect(ctx, "my-registry").
					Return(container.InspectResponse{
						NetworkSettings: &container.NetworkSettings{
							Networks: map[string]*network.EndpointSettings{
								"kind-test": {IPAddress: "172.18.0.5"},
							},
						},
					}, nil).
					Once()
			},
			expectedErr: docker.ErrNotConnectedToNetwork,
		},
		{
			name:          "returns ErrNoIPAddress when IP is empty",
			containerName: "my-registry",
			networkName:   "kind-test",
			setupMock: func(m *docker.MockAPIClient, ctx context.Context) {
				m.EXPECT().
					ContainerInspect(ctx, "my-registry").
					Return(container.InspectResponse{
						NetworkSettings: &container.NetworkSettings{
							Networks: map[string]*network.EndpointSettings{
								"kind-test": {IPAddress: ""},
							},
						},
					}, nil).
					Once()
			},
			expectedErr: docker.ErrNoIPAddress,
		},
		{
			name:          "returns correct IP from multiple networks",
			containerName: "my-registry",
			networkName:   "k3d-cluster",
			setupMock: func(m *docker.MockAPIClient, ctx context.Context) {
				m.EXPECT().
					ContainerInspect(ctx, "my-registry").
					Return(container.InspectResponse{
						NetworkSettings: &container.NetworkSettings{
							Networks: map[string]*network.EndpointSettings{
								"bridge":      {IPAddress: "172.17.0.2"},
								"k3d-cluster": {IPAddress: "172.19.0.3"},
								"kind-test":   {IPAddress: "172.18.0.5"},
							},
						},
					}, nil).
					Once()
			},
			expectedIP: "172.19.0.3",
		},
	}

	for i := range tests {
		testCase := tests[i]

		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			mockClient := docker.NewMockAPIClient(t)
			ctx := context.Background()
			testCase.setupMock(mockClient, ctx)

			//nolint:varnamelen // Short names keep table-driven tests readable.
			ip, err := docker.ResolveContainerIPOnNetwork(
				ctx,
				mockClient,
				testCase.containerName,
				testCase.networkName,
			)

			if testCase.expectedErr != nil {
				require.ErrorIs(t, err, testCase.expectedErr)
				assert.Empty(t, ip)

				return
			}

			if testCase.errContains != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), testCase.errContains)
				assert.Empty(t, ip)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, testCase.expectedIP, ip)
		})
	}
}
