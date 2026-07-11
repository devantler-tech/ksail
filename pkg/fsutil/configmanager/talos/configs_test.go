package talos_test

import (
	"fmt"
	"testing"

	configmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager"
	"github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/talos"
	talosconfig "github.com/siderolabs/talos/pkg/machinery/config"
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

	controlPlane := configs.ControlPlane()
	require.NotNil(t, controlPlane)

	// Talos v1.14 moved host DNS out of machine.features into a dedicated
	// network resolver document; the provider exposes it via the unified
	// NetworkHostDNSConfig accessor (which falls back to the v1alpha1 features
	// config), so KSail's generated config keeps host DNS enabled.
	hostDNS := controlPlane.NetworkHostDNSConfig()
	require.NotNil(t, hostDNS, "HostDNS config should not be nil")

	// These are required for container mode to work properly
	assert.True(t, hostDNS.HostDNSEnabled(), "HostDNS should be enabled for container mode")
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

func TestConfigs_KubernetesVersion(t *testing.T) {
	t.Parallel()

	t.Run("returns the configured version", func(t *testing.T) {
		t.Parallel()

		manager := talos.NewConfigManager("", "kube-version-test", "1.32.5", "10.5.0.0/24")

		configs, err := manager.Load(configmanager.LoadOptions{})
		require.NoError(t, err)
		require.NotNil(t, configs)

		assert.Equal(t, "1.32.5", configs.KubernetesVersion())
	})

	t.Run("returns DefaultKubernetesVersion when empty string passed", func(t *testing.T) {
		t.Parallel()

		// NewConfigManager converts "" to DefaultKubernetesVersion before storing.
		manager := talos.NewConfigManager("", "kube-version-default", "", "10.5.0.0/24")

		configs, err := manager.Load(configmanager.LoadOptions{})
		require.NoError(t, err)
		require.NotNil(t, configs)

		assert.Equal(t, talos.DefaultKubernetesVersion, configs.KubernetesVersion())
	})
}

func TestConfigs_Patches(t *testing.T) {
	t.Parallel()

	t.Run("returns nil when no patches configured", func(t *testing.T) {
		t.Parallel()

		manager := talos.NewConfigManager("", "patches-nil-test", "1.11.2", "10.5.0.0/24")

		configs, err := manager.Load(configmanager.LoadOptions{})
		require.NoError(t, err)
		require.NotNil(t, configs)

		assert.Nil(t, configs.Patches())
	})

	t.Run("returns copy that does not mutate internal state", func(t *testing.T) {
		t.Parallel()

		patchContent := []byte("machine:\n  network:\n    hostname: test\n")
		manager := talos.NewConfigManager("", "patches-copy-test", "1.11.2", "10.5.0.0/24").
			WithAdditionalPatches([]talos.Patch{
				{
					Path:    "test.yaml",
					Scope:   talos.PatchScopeControlPlane,
					Content: patchContent,
				},
			})

		configs, err := manager.Load(configmanager.LoadOptions{})
		require.NoError(t, err)
		require.NotNil(t, configs)

		patches := configs.Patches()
		require.Len(t, patches, 1)
		assert.Equal(t, "test.yaml", patches[0].Path)

		// Mutating the returned slice must not affect the internal state.
		patches[0].Path = "mutated.yaml"
		assert.Equal(t, "test.yaml", configs.Patches()[0].Path, "internal state was mutated")
	})
}

func TestConfigs_WithSecrets(t *testing.T) {
	t.Parallel()

	t.Run("preserves PKI secrets across regeneration", func(t *testing.T) {
		t.Parallel()

		// Create two independent config bundles — they'll have different secrets
		manager1 := talos.NewConfigManager("", "cluster-a", "1.32.0", "10.5.0.0/24")
		configs1, err := manager1.Load(configmanager.LoadOptions{})
		require.NoError(t, err)

		manager2 := talos.NewConfigManager("", "cluster-b", "1.32.0", "10.5.0.0/24")
		configs2, err := manager2.Load(configmanager.LoadOptions{})
		require.NoError(t, err)

		// Extract secrets from config1
		secrets1, err := configs1.ExtractSecrets()
		require.NoError(t, err)
		require.NotNil(t, secrets1)

		// Rebuild config2 with config1's secrets
		rebuilt, err := configs2.WithSecrets(secrets1)
		require.NoError(t, err)

		// The rebuilt config should have the same OS CA as config1
		originalOSCA := configs1.ControlPlane().Machine().Security().IssuingCA().Crt
		rebuiltOSCA := rebuilt.ControlPlane().Machine().Security().IssuingCA().Crt
		assert.Equal(t, originalOSCA, rebuiltOSCA, "OS CA should match after WithSecrets")

		// The rebuilt config should have the same cluster CA as config1
		originalClusterCA := configs1.ControlPlane().Cluster().IssuingCA().Crt
		rebuiltClusterCA := rebuilt.ControlPlane().Cluster().IssuingCA().Crt
		assert.Equal(
			t,
			originalClusterCA,
			rebuiltClusterCA,
			"cluster CA should match after WithSecrets",
		)

		// The rebuilt config should have the same machine token as config1
		originalToken := configs1.ControlPlane().Machine().Security().Token()
		rebuiltToken := rebuilt.ControlPlane().Machine().Security().Token()
		assert.Equal(t, originalToken, rebuiltToken, "machine token should match after WithSecrets")

		// The rebuilt config should still have config2's cluster name
		assert.Equal(t, "cluster-b", rebuilt.GetClusterName())
	})

	t.Run("returns same config when secrets is nil", func(t *testing.T) {
		t.Parallel()

		manager := talos.NewConfigManager("", "my-cluster", "1.32.0", "10.5.0.0/24")
		configs, err := manager.Load(configmanager.LoadOptions{})
		require.NoError(t, err)

		same, err := configs.WithSecrets(nil)
		require.NoError(t, err)
		assert.Same(t, configs, same, "should return same config when secrets is nil")
	})
}

func TestConfigs_WithKubernetesVersion(t *testing.T) {
	t.Parallel()

	t.Run("regenerates at the new version preserving PKI", func(t *testing.T) {
		t.Parallel()

		manager := talos.NewConfigManager("", "k8s-version-cluster", "1.32.0", "10.5.0.0/24")
		configs, err := manager.Load(configmanager.LoadOptions{})
		require.NoError(t, err)

		rebuilt, err := configs.WithKubernetesVersion("1.33.0")
		require.NoError(t, err)

		assert.Equal(t, "1.33.0", rebuilt.KubernetesVersion())
		assert.Contains(t, rebuilt.ControlPlane().K8sAPIServerConfig().Image(), ":v1.33.0")

		// PKI is preserved so the regenerated config still matches the cluster.
		assert.Equal(t,
			configs.ControlPlane().Cluster().IssuingCA().Crt,
			rebuilt.ControlPlane().Cluster().IssuingCA().Crt,
			"cluster CA should be preserved across version change",
		)
		assert.Equal(t, "k8s-version-cluster", rebuilt.GetClusterName())
	})

	t.Run("normalises a v prefix", func(t *testing.T) {
		t.Parallel()

		manager := talos.NewConfigManager("", "k8s-version-vprefix", "1.32.0", "10.5.0.0/24")
		configs, err := manager.Load(configmanager.LoadOptions{})
		require.NoError(t, err)

		rebuilt, err := configs.WithKubernetesVersion("v1.34.1")
		require.NoError(t, err)

		assert.Equal(t, "1.34.1", rebuilt.KubernetesVersion())
	})

	t.Run("returns same config for empty or matching version", func(t *testing.T) {
		t.Parallel()

		manager := talos.NewConfigManager("", "k8s-version-noop", "1.32.0", "10.5.0.0/24")
		configs, err := manager.Load(configmanager.LoadOptions{})
		require.NoError(t, err)

		same, err := configs.WithKubernetesVersion("")
		require.NoError(t, err)
		assert.Same(t, configs, same, "empty version should be a no-op")

		same, err = configs.WithKubernetesVersion("v1.32.0")
		require.NoError(t, err)
		assert.Same(t, configs, same, "matching version should be a no-op")
	})
}

func TestConfigs_WithCertSANs(t *testing.T) {
	t.Parallel()

	t.Run("adds exposure address to API server cert SANs and preserves PKI", func(t *testing.T) {
		t.Parallel()

		manager := talos.NewConfigManager("", "cluster-a", "1.32.0", "10.5.0.0/24")
		configs, err := manager.Load(configmanager.LoadOptions{})
		require.NoError(t, err)

		updated, err := configs.WithCertSANs([]string{"127.0.0.1", "localhost", "5.6.7.8"})
		require.NoError(t, err)

		sans := updated.ControlPlane().K8sAPIServerConfig().CertSANs()
		assert.Contains(t, sans, "5.6.7.8")
		assert.Contains(t, sans, "127.0.0.1")

		// PKI must be preserved across regeneration.
		assert.Equal(t,
			configs.ControlPlane().Cluster().IssuingCA().Crt,
			updated.ControlPlane().Cluster().IssuingCA().Crt,
		)
		assert.Equal(t, "cluster-a", updated.GetClusterName())
	})

	t.Run("returns same config when sans is empty", func(t *testing.T) {
		t.Parallel()

		manager := talos.NewConfigManager("", "my-cluster", "1.32.0", "10.5.0.0/24")
		configs, err := manager.Load(configmanager.LoadOptions{})
		require.NoError(t, err)

		same, err := configs.WithCertSANs(nil)
		require.NoError(t, err)
		assert.Same(t, configs, same)
	})

	t.Run("adds API server SANs to Talos 1.14 multi-document config", func(t *testing.T) {
		t.Parallel()

		manager := talos.NewConfigManager(
			t.TempDir(),
			"cluster-114",
			"1.36.0",
			"10.5.0.0/24",
		).WithVersionContract(talosconfig.TalosVersion1_14)
		configs, err := manager.Load(configmanager.LoadOptions{})
		require.NoError(t, err)

		updated, err := configs.WithCertSANs([]string{"127.0.0.1", "203.0.113.10"})
		require.NoError(t, err)

		assert.Contains(t, updated.ControlPlane().K8sAPIServerConfig().CertSANs(), "203.0.113.10")
	})
}

func TestConfigs_WithHetznerVIP(t *testing.T) {
	t.Parallel()

	t.Run("renders the VIP block on control planes only and preserves PKI", func(t *testing.T) {
		t.Parallel()

		manager := talos.NewConfigManager("", "vip-cluster", "1.32.0", "10.5.0.0/24")
		configs, err := manager.Load(configmanager.LoadOptions{})
		require.NoError(t, err)

		updated, err := configs.WithHetznerVIP("192.0.2.10", "test-token")
		require.NoError(t, err)

		devices := updated.ControlPlane().Machine().Network().Devices()
		require.Len(t, devices, 1)
		assert.Equal(t, "eth0", devices[0].Interface())
		assert.True(t, devices[0].DHCP(), "declaring the interface must keep DHCP addressing")

		vip := devices[0].VIPConfig()
		require.NotNil(t, vip)
		assert.Equal(t, "192.0.2.10", vip.IP())
		require.NotNil(t, vip.HCloud())
		assert.Equal(t, "test-token", vip.HCloud().APIToken())

		// The patch is control-plane-scoped: workers carry no VIP interface.
		assert.Empty(t, updated.Worker().Machine().Network().Devices())

		// PKI must be preserved across regeneration.
		assert.Equal(t,
			configs.ControlPlane().Cluster().IssuingCA().Crt,
			updated.ControlPlane().Cluster().IssuingCA().Crt,
		)
		assert.Equal(t, "vip-cluster", updated.GetClusterName())
	})

	t.Run("returns same config when vip is empty", func(t *testing.T) {
		t.Parallel()

		manager := talos.NewConfigManager("", "vip-noop", "1.32.0", "10.5.0.0/24")
		configs, err := manager.Load(configmanager.LoadOptions{})
		require.NoError(t, err)

		same, err := configs.WithHetznerVIP("", "test-token")
		require.NoError(t, err)
		assert.Same(t, configs, same)
	})

	t.Run("errors when the token is empty", func(t *testing.T) {
		t.Parallel()

		manager := talos.NewConfigManager("", "vip-no-token", "1.32.0", "10.5.0.0/24")
		configs, err := manager.Load(configmanager.LoadOptions{})
		require.NoError(t, err)

		_, err = configs.WithHetznerVIP("192.0.2.10", "")
		require.ErrorIs(t, err, talos.ErrHetznerVIPTokenRequired)
	})
}

func TestConfigs_ExtractSecrets(t *testing.T) {
	t.Parallel()

	t.Run("extracts secrets from loaded config", func(t *testing.T) {
		t.Parallel()

		manager := talos.NewConfigManager("", "extract-test", "1.32.0", "10.5.0.0/24")
		configs, err := manager.Load(configmanager.LoadOptions{})
		require.NoError(t, err)

		secretsBundle, err := configs.ExtractSecrets()
		require.NoError(t, err)
		require.NotNil(t, secretsBundle)
		assert.NotNil(t, secretsBundle.Certs)
		assert.NotNil(t, secretsBundle.Certs.K8s)
		assert.NotNil(t, secretsBundle.Certs.Etcd)
		assert.NotNil(t, secretsBundle.Certs.OS)
		assert.NotEmpty(t, secretsBundle.TrustdInfo.Token)
	})

	t.Run("returns nil when bundle is nil", func(t *testing.T) {
		t.Parallel()

		configs := &talos.Configs{}
		bundle, err := configs.ExtractSecrets()
		require.NoError(t, err)
		assert.Nil(t, bundle)
	})
}
