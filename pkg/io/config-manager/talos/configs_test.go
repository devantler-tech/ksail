package talos_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/io/config-manager/talos"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigs_GetClusterName(t *testing.T) {
	t.Parallel()

	manager := talos.NewConfigManager("", "my-test-cluster", "1.32.0", "10.5.0.0/24")

	configs, err := manager.LoadConfig(nil)
	require.NoError(t, err)
	require.NotNil(t, configs)

	assert.Equal(t, "my-test-cluster", configs.GetClusterName())
}

func TestConfigs_Bundle(t *testing.T) {
	t.Parallel()

	manager := talos.NewConfigManager("", "bundle-test", "1.32.0", "10.5.0.0/24")

	configs, err := manager.LoadConfig(nil)
	require.NoError(t, err)
	require.NotNil(t, configs)

	bundle := configs.Bundle()
	require.NotNil(t, bundle)
}

func TestConfigs_ControlPlane(t *testing.T) {
	t.Parallel()

	manager := talos.NewConfigManager("", "cp-test", "1.32.0", "10.5.0.0/24")

	configs, err := manager.LoadConfig(nil)
	require.NoError(t, err)
	require.NotNil(t, configs)

	cp := configs.ControlPlane()
	require.NotNil(t, cp)
	assert.NotNil(t, cp.Machine())
	assert.NotNil(t, cp.Cluster())
}

func TestConfigs_Worker(t *testing.T) {
	t.Parallel()

	manager := talos.NewConfigManager("", "worker-test", "1.32.0", "10.5.0.0/24")

	configs, err := manager.LoadConfig(nil)
	require.NoError(t, err)
	require.NotNil(t, configs)

	worker := configs.Worker()
	require.NotNil(t, worker)
	assert.NotNil(t, worker.Machine())
	assert.NotNil(t, worker.Cluster())
}

func TestConfigs_IsCNIDisabled_DefaultCNI(t *testing.T) {
	t.Parallel()

	manager := talos.NewConfigManager("", "cni-default", "1.32.0", "10.5.0.0/24")

	configs, err := manager.LoadConfig(nil)
	require.NoError(t, err)
	require.NotNil(t, configs)

	// Default CNI should not be disabled
	assert.False(t, configs.IsCNIDisabled())
}

func TestConfigs_NetworkCIDR(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		networkCIDR     string
		expectedDefault string
	}{
		{
			name:            "custom CIDR",
			networkCIDR:     "10.6.0.0/24",
			expectedDefault: talos.DefaultNetworkCIDR,
		},
		{
			name:            "default CIDR",
			networkCIDR:     "",
			expectedDefault: talos.DefaultNetworkCIDR,
		},
	}

	for i := range tests {
		testCase := tests[i]

		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			manager := talos.NewConfigManager("", "network-test", "1.32.0", testCase.networkCIDR)

			configs, err := manager.LoadConfig(nil)
			require.NoError(t, err)
			require.NotNil(t, configs)

			cidr := configs.NetworkCIDR()
			assert.NotEmpty(t, cidr)
		})
	}
}

func TestConfigs_ApplyMirrorRegistries_EmptyMirrors(t *testing.T) {
	t.Parallel()

	manager := talos.NewConfigManager("", "mirror-empty", "1.32.0", "10.5.0.0/24")

	configs, err := manager.LoadConfig(nil)
	require.NoError(t, err)
	require.NotNil(t, configs)

	err = configs.ApplyMirrorRegistries(nil)
	require.NoError(t, err)

	err = configs.ApplyMirrorRegistries([]talos.MirrorRegistry{})
	require.NoError(t, err)
}

func TestConfigs_ApplyMirrorRegistries_WithMirrors(t *testing.T) {
	t.Parallel()

	manager := talos.NewConfigManager("", "mirror-test", "1.32.0", "10.5.0.0/24")

	configs, err := manager.LoadConfig(nil)
	require.NoError(t, err)
	require.NotNil(t, configs)

	mirrors := []talos.MirrorRegistry{
		{
			Host:      "docker.io",
			Endpoints: []string{"http://localhost:5000"},
		},
		{
			Host:      "ghcr.io",
			Endpoints: []string{"http://localhost:5001"},
		},
	}

	err = configs.ApplyMirrorRegistries(mirrors)
	require.NoError(t, err)
}

func TestConfigs_ApplyMirrorRegistries_EmptyHost(t *testing.T) {
	t.Parallel()

	manager := talos.NewConfigManager("", "mirror-empty-host", "1.32.0", "10.5.0.0/24")

	configs, err := manager.LoadConfig(nil)
	require.NoError(t, err)
	require.NotNil(t, configs)

	// Mirror with empty host should be skipped
	mirrors := []talos.MirrorRegistry{
		{
			Host:      "",
			Endpoints: []string{"http://localhost:5000"},
		},
	}

	err = configs.ApplyMirrorRegistries(mirrors)
	require.NoError(t, err)
}

func TestMirrorRegistry_Structure(t *testing.T) {
	t.Parallel()

	mirror := talos.MirrorRegistry{
		Host:      "docker.io",
		Endpoints: []string{"http://localhost:5000", "http://localhost:5001"},
	}

	assert.Equal(t, "docker.io", mirror.Host)
	assert.Equal(t, []string{"http://localhost:5000", "http://localhost:5001"}, mirror.Endpoints)
}

func TestConfigs_ApplyKubeletCertRotation(t *testing.T) {
	t.Parallel()

	manager := talos.NewConfigManager("", "kubelet-cert-rotation", "1.32.0", "10.5.0.0/24")

	configs, err := manager.LoadConfig(nil)
	require.NoError(t, err)
	require.NotNil(t, configs)

	// Apply kubelet cert rotation
	err = configs.ApplyKubeletCertRotation()
	require.NoError(t, err)

	// Verify both control-plane and worker configs exist
	require.NotNil(t, configs.ControlPlane())
	require.NotNil(t, configs.Worker())

	// Verify the rotate-server-certificates flag was applied to control-plane
	cpKubelet := configs.ControlPlane().Machine().Kubelet()
	require.NotNil(t, cpKubelet, "control-plane kubelet config should not be nil")
	cpExtraArgs := cpKubelet.ExtraArgs()
	require.NotNil(t, cpExtraArgs, "control-plane kubelet extra args should not be nil")
	assert.Equal(t, "true", cpExtraArgs["rotate-server-certificates"],
		"control-plane should have rotate-server-certificates=true")

	// Verify the rotate-server-certificates flag was applied to worker
	workerKubelet := configs.Worker().Machine().Kubelet()
	require.NotNil(t, workerKubelet, "worker kubelet config should not be nil")
	workerExtraArgs := workerKubelet.ExtraArgs()
	require.NotNil(t, workerExtraArgs, "worker kubelet extra args should not be nil")
	assert.Equal(t, "true", workerExtraArgs["rotate-server-certificates"],
		"worker should have rotate-server-certificates=true")
}

func TestConfigs_ApplyKubeletCertRotation_Idempotent(t *testing.T) {
	t.Parallel()

	manager := talos.NewConfigManager("", "kubelet-cert-idempotent", "1.32.0", "10.5.0.0/24")

	configs, err := manager.LoadConfig(nil)
	require.NoError(t, err)
	require.NotNil(t, configs)

	// Apply kubelet cert rotation twice - should not error
	err = configs.ApplyKubeletCertRotation()
	require.NoError(t, err)

	err = configs.ApplyKubeletCertRotation()
	require.NoError(t, err)
}
