package talosprovisioner_test

import (
	"fmt"
	"testing"

	configmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager"
	talos "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/talos"
	dockerprovider "github.com/devantler-tech/ksail/v7/pkg/svc/provider/docker"
	hetzner "github.com/devantler-tech/ksail/v7/pkg/svc/provider/hetzner"
	talosprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/talos"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

func TestClusterReadinessChecks_HetznerCertRotationSkipsDiagnostics(t *testing.T) {
	t.Parallel()

	certRotationConfigs := loadCertRotationConfigs(t, "hetzner-cert-rotation", "true")

	opts := talosprovisioner.NewOptions()
	opts.SkipCNIChecks = true

	hetznerWithCertRotation := talosprovisioner.NewProvisioner(nil, opts).
		WithInfraProvider(&hetzner.Provider{}).
		WithTalosConfigsForTest(certRotationConfigs)
	hetznerWithoutCertRotation := talosprovisioner.NewProvisioner(nil, opts).
		WithInfraProvider(&hetzner.Provider{})

	withRotation := hetznerWithCertRotation.ClusterReadinessChecksCountForTest()
	withoutRotation := hetznerWithoutCertRotation.ClusterReadinessChecksCountForTest()

	assert.Less(t, withRotation, withoutRotation,
		"Hetzner with cert rotation should skip NoDiagnostics check (fewer total checks)")
}

func loadCertRotationConfigs(t *testing.T, clusterName, rotateValue string) *talos.Configs {
	t.Helper()

	patchContent := fmt.Appendf(nil, `machine:
  kubelet:
    extraArgs:
      rotate-server-certificates: "%s"
`, rotateValue)

	manager := talos.NewConfigManager("", clusterName, "1.32.0", "10.5.0.0/24")
	manager = manager.WithAdditionalPatches([]talos.Patch{
		{
			Path:    "test-cert-rotation.yaml",
			Scope:   talos.PatchScopeControlPlane,
			Content: patchContent,
		},
	})

	configs, err := manager.Load(configmanager.LoadOptions{})
	require.NoError(t, err)
	require.NotNil(t, configs)

	return configs
}
