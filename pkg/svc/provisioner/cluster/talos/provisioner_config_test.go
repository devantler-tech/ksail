package talosprovisioner_test

import (
	"testing"

	dockerprovider "github.com/devantler-tech/ksail/v7/pkg/svc/provider/docker"
	hetzner "github.com/devantler-tech/ksail/v7/pkg/svc/provider/hetzner"
	talosprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/talos"
	"github.com/stretchr/testify/assert"
)

// --- isDockerProvider ---

func TestIsDockerProvider(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		prov func() *talosprovisioner.Provisioner
		want bool
	}{
		{
			name: "nil infra provider is Docker (legacy default)",
			prov: func() *talosprovisioner.Provisioner {
				return talosprovisioner.NewProvisioner(nil, nil)
			},
			want: true,
		},
		{
			name: "explicit Docker provider returns true",
			prov: func() *talosprovisioner.Provisioner {
				return talosprovisioner.NewProvisioner(nil, nil).
					WithInfraProvider(&dockerprovider.Provider{})
			},
			want: true,
		},
		{
			name: "Hetzner provider returns false",
			prov: func() *talosprovisioner.Provisioner {
				return talosprovisioner.NewProvisioner(nil, nil).
					WithInfraProvider(&hetzner.Provider{})
			},
			want: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, tc.prov().IsDockerProviderForTest())
		})
	}
}

// --- clusterReadinessChecks provider selection ---

func TestClusterReadinessChecks_DockerUsesFewerChecks(t *testing.T) {
	t.Parallel()

	opts := talosprovisioner.NewOptions()
	opts.SkipCNIChecks = true

	dockerProvisioner := talosprovisioner.NewProvisioner(nil, opts)
	hetznerProvisioner := talosprovisioner.NewProvisioner(nil, opts).
		WithInfraProvider(&hetzner.Provider{})

	dockerCount := dockerProvisioner.ClusterReadinessChecksCountForTest()
	hetznerCount := hetznerProvisioner.ClusterReadinessChecksCountForTest()

	assert.Less(t, dockerCount, hetznerCount,
		"Docker provider should use fewer pre-boot checks than Hetzner provider")
}
