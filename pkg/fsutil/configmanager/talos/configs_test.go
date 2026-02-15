package talos_test

import (
	"fmt"
	"testing"

	configmanager "github.com/devantler-tech/ksail/v5/pkg/fsutil/configmanager"
	"github.com/devantler-tech/ksail/v5/pkg/fsutil/configmanager/talos"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigs_GetClusterName(t *testing.T) {
	t.Parallel()

	manager := talos.NewConfigManager("", "my-test-cluster", "1.32.0", "10.5.0.0/24")

	configs, err := manager.Load(configmanager.LoadOptions{})
	require.NoError(t, err)
	require.NotNil(t, configs)

	assert.Equal(t, "my-test-cluster", configs.GetClusterName())
}

func TestConfigs_Bundle(t *testing.T) {
	t.Parallel()

	manager := talos.NewConfigManager("", "bundle-test", "1.32.0", "10.5.0.0/24")

	configs, err := manager.Load(configmanager.LoadOptions{})
	require.NoError(t, err)
	require.NotNil(t, configs)

	bundle := configs.Bundle()
	require.NotNil(t, bundle)
}

func TestConfigs_ControlPlane(t *testing.T) {
	t.Parallel()

	manager := talos.NewConfigManager("", "cp-test", "1.32.0", "10.5.0.0/24")

	configs, err := manager.Load(configmanager.LoadOptions{})
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

	configs, err := manager.Load(configmanager.LoadOptions{})
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

	configs, err := manager.Load(configmanager.LoadOptions{})
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

			configs, err := manager.Load(configmanager.LoadOptions{})
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

	configs, err := manager.Load(configmanager.LoadOptions{})
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

	configs, err := manager.Load(configmanager.LoadOptions{})
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

	configs, err := manager.Load(configmanager.LoadOptions{})
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

// TestConfigs_HostDNS_Enabled verifies that hostDNS features are enabled by default.
// This is critical for Talos-in-Docker (container mode) to work properly.
// Without hostDNS.enabled and hostDNS.forwardKubeDNSToHost, DNS resolution
// inside the Talos container will fail, preventing image pulls.
func TestConfigs_HostDNS_Enabled(t *testing.T) {
	t.Parallel()

	manager := talos.NewConfigManager("", "hostdns-test", "1.32.0", "10.5.0.0/24")

	configs, err := manager.Load(configmanager.LoadOptions{})
	require.NoError(t, err)
	require.NotNil(t, configs)

	cp := configs.ControlPlane()
	require.NotNil(t, cp)

	features := cp.Machine().Features()
	require.NotNil(t, features, "Machine features should not be nil")

	hostDNS := features.HostDNS()
	require.NotNil(t, hostDNS, "HostDNS config should not be nil")

	// These are required for container mode to work properly
	assert.True(t, hostDNS.Enabled(), "HostDNS should be enabled for container mode")
	assert.True(
		t,
		hostDNS.ForwardKubeDNSToHost(),
		"HostDNS should forward kube-dns to host for container mode",
	)
}

func TestConfigs_WithName(t *testing.T) {
	t.Parallel()

	t.Run("changes cluster name and regenerates bundle", func(t *testing.T) {
		t.Parallel()

		manager := talos.NewConfigManager("", "original-cluster", "1.32.0", "10.5.0.0/24")

		originalConfigs, err := manager.Load(configmanager.LoadOptions{})
		require.NoError(t, err)
		require.NotNil(t, originalConfigs)

		assert.Equal(t, "original-cluster", originalConfigs.GetClusterName())

		// Rename the cluster
		renamedConfigs, err := originalConfigs.WithName("renamed-cluster")
		require.NoError(t, err)
		require.NotNil(t, renamedConfigs)

		// Verify the new config has the new name
		assert.Equal(t, "renamed-cluster", renamedConfigs.GetClusterName())

		// Verify the bundle was regenerated (different PKI)
		originalBundle := originalConfigs.Bundle()
		renamedBundle := renamedConfigs.Bundle()
		assert.NotSame(t, originalBundle, renamedBundle, "bundle should be regenerated, not reused")

		// Verify the talosconfig context is updated
		originalTalosConfig := originalBundle.TalosConfig()
		renamedTalosConfig := renamedBundle.TalosConfig()

		assert.Equal(t, "original-cluster", originalTalosConfig.Context)
		assert.Equal(t, "renamed-cluster", renamedTalosConfig.Context)
	})

	t.Run("returns same config when name is empty", func(t *testing.T) {
		t.Parallel()

		manager := talos.NewConfigManager("", "my-cluster", "1.32.0", "10.5.0.0/24")

		originalConfigs, err := manager.Load(configmanager.LoadOptions{})
		require.NoError(t, err)

		sameConfigs, err := originalConfigs.WithName("")
		require.NoError(t, err)
		assert.Same(t, originalConfigs, sameConfigs, "should return same config when name is empty")
	})

	t.Run("returns same config when name is unchanged", func(t *testing.T) {
		t.Parallel()

		manager := talos.NewConfigManager("", "my-cluster", "1.32.0", "10.5.0.0/24")

		originalConfigs, err := manager.Load(configmanager.LoadOptions{})
		require.NoError(t, err)

		sameConfigs, err := originalConfigs.WithName("my-cluster")
		require.NoError(t, err)
		assert.Same(
			t,
			originalConfigs,
			sameConfigs,
			"should return same config when name is unchanged",
		)
	})
}

func TestConfigs_IsKubeletCertRotationEnabled(t *testing.T) {
	t.Parallel()

	t.Run("returns false when cert rotation not configured", func(t *testing.T) {
		t.Parallel()

		configs := loadConfigsWithoutPatch(t, "cert-rotation-test")

		// Default config should not have cert rotation enabled
		assert.False(t, configs.IsKubeletCertRotationEnabled())
	})

	t.Run("returns true when cert rotation is enabled via patch", func(t *testing.T) {
		t.Parallel()

		configs := loadConfigsWithCertRotationPatch(t, "cert-rotation-enabled", "true")

		// Should detect cert rotation is enabled
		assert.True(t, configs.IsKubeletCertRotationEnabled())
	})

	t.Run("returns false when rotate-server-certificates is false", func(t *testing.T) {
		t.Parallel()

		configs := loadConfigsWithCertRotationPatch(t, "cert-rotation-disabled", "false")

		// Should detect cert rotation is not enabled
		assert.False(t, configs.IsKubeletCertRotationEnabled())
	})
}

func loadConfigsWithoutPatch(t *testing.T, clusterName string) *talos.Configs {
	t.Helper()

	manager := talos.NewConfigManager("", clusterName, "1.32.0", "10.5.0.0/24")

	configs, err := manager.Load(configmanager.LoadOptions{})
	require.NoError(t, err)
	require.NotNil(t, configs)

	return configs
}

func loadConfigsWithCertRotationPatch(
	t *testing.T,
	clusterName, rotateValue string,
) *talos.Configs {
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
