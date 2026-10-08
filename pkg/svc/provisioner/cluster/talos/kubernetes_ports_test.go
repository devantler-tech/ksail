package talosprovisioner_test

import (
	"context"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/client/docker"
	talosprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/talos"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/go-connections/nat"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestNestedTalosBootstrapSelectsLoopbackBinding(t *testing.T) {
	t.Parallel()

	for _, serviceFirst := range []bool{false, true} {
		name := "loopback_first"
		if serviceFirst {
			name = "service_first"
		}

		t.Run(name, func(t *testing.T) {
			t.Parallel()

			bindings := []nat.PortBinding{
				{HostIP: "127.0.0.1", HostPort: "44444"},
				{HostIP: "10.42.0.5", HostPort: "55555"},
			}
			if serviceFirst {
				bindings[0], bindings[1] = bindings[1], bindings[0]
			}

			client := docker.NewMockAPIClient(t)
			client.EXPECT().ContainerList(mock.Anything, mock.Anything).
				Return([]container.Summary{{ID: "control-plane"}}, nil)
			client.EXPECT().ContainerInspect(mock.Anything, "control-plane").
				Return(container.InspectResponse{
					NetworkSettings: &container.NetworkSettings{
						NetworkSettingsBase: container.NetworkSettingsBase{ //nolint:staticcheck
							Ports: nat.PortMap{
								"50000/tcp": {{HostIP: "127.0.0.1", HostPort: "33333"}},
								"6443/tcp":  bindings,
							},
						},
					},
				}, nil)
			inner := talosprovisioner.NewProvisioner(createTestTalosConfigs(t, "demo"), nil).
				WithDockerClient(client)
			prov, err := talosprovisioner.NewKubernetesProvisioner(
				talosprovisioner.KubernetesProvisionerConfig{
					InnerProvisioner: inner,
					ClusterName:      "demo",
				},
			)
			require.NoError(t, err)

			talosPort, kubernetesPort, err := prov.DiscoverMappedPortsForTest(
				context.Background(),
				"demo",
			)
			require.NoError(t, err)
			assert.Equal(t, 33333, talosPort)
			assert.Equal(t, 44444, kubernetesPort)

			servicePort, err := prov.DiscoverServicePortForTest(
				context.Background(),
				"demo",
				"10.42.0.5",
			)
			require.NoError(t, err)
			assert.Equal(t, 55555, servicePort)
		})
	}
}

func TestNestedTalosServiceRejectsUnusableBindings(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name     string
		bindings []nat.PortBinding
	}{
		{"missing", nil},
		{"loopback_only", []nat.PortBinding{{HostIP: "127.0.0.1", HostPort: "44444"}}},
		{"wildcard", []nat.PortBinding{{HostIP: "0.0.0.0", HostPort: "55555"}}},
		{"another_pod", []nat.PortBinding{{HostIP: "10.42.0.6", HostPort: "55555"}}},
		{"empty_port", []nat.PortBinding{{HostIP: "10.42.0.5", HostPort: ""}}},
		{"zero_port", []nat.PortBinding{{HostIP: "10.42.0.5", HostPort: "0"}}},
		{"out_of_range", []nat.PortBinding{{HostIP: "10.42.0.5", HostPort: "65536"}}},
		{"malformed", []nat.PortBinding{{HostIP: "10.42.0.5", HostPort: "wrong"}}},
		{"ambiguous", []nat.PortBinding{
			{HostIP: "10.42.0.5", HostPort: "55555"},
			{HostIP: "10.42.0.5", HostPort: "55556"},
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			client := docker.NewMockAPIClient(t)
			client.EXPECT().ContainerList(mock.Anything, mock.Anything).
				Return([]container.Summary{{ID: "control-plane"}}, nil)
			client.EXPECT().ContainerInspect(mock.Anything, "control-plane").
				Return(container.InspectResponse{
					NetworkSettings: &container.NetworkSettings{
						NetworkSettingsBase: container.NetworkSettingsBase{ //nolint:staticcheck
							Ports: nat.PortMap{"6443/tcp": testCase.bindings},
						},
					},
				}, nil)
			inner := talosprovisioner.NewProvisioner(createTestTalosConfigs(t, "demo"), nil).
				WithDockerClient(client)
			prov, err := talosprovisioner.NewKubernetesProvisioner(
				talosprovisioner.KubernetesProvisionerConfig{
					InnerProvisioner: inner,
					ClusterName:      "demo",
				},
			)
			require.NoError(t, err)

			port, err := prov.DiscoverServicePortForTest(context.Background(), "demo", "10.42.0.5")
			require.Error(t, err)
			assert.Zero(t, port)
		})
	}
}
