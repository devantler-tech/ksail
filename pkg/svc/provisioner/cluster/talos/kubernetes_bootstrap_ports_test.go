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

func newNestedBootstrapPortReader(
	t *testing.T,
	bindings []nat.PortBinding,
) *talosprovisioner.KubernetesProvisioner {
	t.Helper()

	client := docker.NewMockAPIClient(t)
	client.EXPECT().ContainerList(mock.Anything, mock.Anything).
		Return([]container.Summary{{ID: "control-plane"}}, nil)
	client.EXPECT().ContainerInspect(mock.Anything, "control-plane").
		Return(container.InspectResponse{
			NetworkSettings: &container.NetworkSettings{
				NetworkSettingsBase: container.NetworkSettingsBase{ //nolint:staticcheck
					Ports: nat.PortMap{
						"50000/tcp": {{HostIP: "0.0.0.0", HostPort: "33333"}},
						"6443/tcp":  bindings,
					},
				},
			},
		}, nil)
	inner := talosprovisioner.NewProvisioner(createTestTalosConfigs(t, "demo"), nil).
		WithDockerClient(client)
	prov, err := talosprovisioner.NewKubernetesProvisioner(
		talosprovisioner.KubernetesProvisionerConfig{InnerProvisioner: inner, ClusterName: "demo"},
	)
	require.NoError(t, err)

	return prov
}

func TestNestedTalosBootstrapAcceptsSDKWildcard(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name     string
		bindings []nat.PortBinding
	}{
		{"wildcard_first", []nat.PortBinding{
			{HostIP: "0.0.0.0", HostPort: "44444"},
			{HostIP: "10.42.0.5", HostPort: "55555"},
		}},
		{"pod_first", []nat.PortBinding{
			{HostIP: "10.42.0.5", HostPort: "55555"},
			{HostIP: "0.0.0.0", HostPort: "44444"},
		}},
		{"prefers_loopback", []nat.PortBinding{
			{HostIP: "0.0.0.0", HostPort: "44445"},
			{HostIP: "10.42.0.5", HostPort: "55555"},
			{HostIP: "127.0.0.1", HostPort: "44444"},
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			prov := newNestedBootstrapPortReader(t, testCase.bindings)
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

func TestNestedTalosBootstrapRejectsUnusableWildcard(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name     string
		bindings []nat.PortBinding
	}{
		{"pod_only", []nat.PortBinding{{HostIP: "10.42.0.5", HostPort: "55555"}}},
		{"malformed", []nat.PortBinding{{HostIP: "0.0.0.0", HostPort: "wrong"}}},
		{"ambiguous", []nat.PortBinding{
			{HostIP: "0.0.0.0", HostPort: "44444"},
			{HostIP: "0.0.0.0", HostPort: "44445"},
		}},
		{"ambiguous_loopback", []nat.PortBinding{
			{HostIP: "127.0.0.1", HostPort: "44444"},
			{HostIP: "127.0.0.1", HostPort: "44445"},
			{HostIP: "0.0.0.0", HostPort: "44446"},
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			prov := newNestedBootstrapPortReader(t, testCase.bindings)
			_, _, err := prov.DiscoverMappedPortsForTest(context.Background(), "demo")
			require.Error(t, err)
		})
	}
}
