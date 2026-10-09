package talos_test

import (
	"bytes"
	"fmt"
	"net/netip"
	"testing"

	configmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager"
	"github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/talos"
	talosconfig "github.com/siderolabs/talos/pkg/machinery/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfigs_WithKubernetesNetworkOverridesRoleNetworks(t *testing.T) {
	t.Parallel()

	for _, contract := range []*talosconfig.VersionContract{talosconfig.TalosVersion1_12, talosconfig.TalosVersion1_14} {
		t.Run(contract.String(), func(t *testing.T) {
			t.Parallel()

			original := loadTalosRoleNetworkFixture(t, contract)
			updated, err := original.WithKubernetesNetwork("10.72.0.0/16", "10.136.0.0/16")
			require.NoError(t, err)

			for _, node := range []talosconfig.Config{updated.ControlPlane(), updated.Worker()} {
				assert.Equal(
					t,
					[]netip.Prefix{netip.MustParsePrefix("10.72.0.0/16")},
					node.K8sNetworkConfig().PodCIDRs(),
				)
				assert.Equal(
					t,
					[]netip.Prefix{netip.MustParsePrefix("10.136.0.0/16")},
					node.K8sNetworkConfig().ServiceCIDRs(),
				)
				assert.Contains(t, node.Machine().Security().CertSANs(), "role.example.test")
			}
		})
	}
}

func loadTalosRoleNetworkFixture(
	t *testing.T,
	contract *talosconfig.VersionContract,
) *talos.Configs {
	t.Helper()

	network := "cluster:\n  network:\n    podSubnets: [10.244.0.0/16]\n    serviceSubnets: [10.96.0.0/12]\n"
	if contract.MultidocKubernetesConfigSupported() {
		network = "apiVersion: v1alpha1\nkind: KubeNetworkConfig\n" +
			"podSubnets: [10.244.0.0/16]\nserviceSubnets: [10.96.0.0/12]\n"
	}

	patches := make([]talos.Patch, 0, 4)
	for _, scope := range []talos.PatchScope{talos.PatchScopeControlPlane, talos.PatchScopeWorker} {
		patches = append(
			patches,
			talos.Patch{
				Path:    fmt.Sprintf("network-%d", scope),
				Scope:   scope,
				Content: []byte(network),
			},
			talos.Patch{
				Path:    fmt.Sprintf("san-%d", scope),
				Scope:   scope,
				Content: []byte("machine:\n  certSANs: [role.example.test]\n"),
			},
		)
	}

	manager := talos.NewConfigManager(t.TempDir(), "role-network", "1.35.0", "10.5.0.0/24").
		WithVersionContract(contract).WithAdditionalPatches(patches)
	configs, err := manager.Load(configmanager.LoadOptions{})
	require.NoError(t, err)

	return configs
}

func TestConfigs_WithKubernetesNetworkPreservesIdentity(t *testing.T) {
	t.Parallel()

	for _, contract := range []*talosconfig.VersionContract{talosconfig.TalosVersion1_12, talosconfig.TalosVersion1_14} {
		t.Run(contract.String(), func(t *testing.T) {
			t.Parallel()
			original := loadTalosNetworkIdentityFixture(t, contract)
			updated, err := original.WithKubernetesNetwork("10.72.0.0/16", "10.136.0.0/16")
			require.NoError(t, err)
			assert.Equal(
				t,
				[]netip.Prefix{netip.MustParsePrefix("10.244.0.0/16")},
				original.ControlPlane().K8sNetworkConfig().PodCIDRs(),
			)
			assert.Equal(
				t,
				original.ControlPlane().Cluster().Endpoint(),
				updated.ControlPlane().Cluster().Endpoint(),
			)
			assert.Equal(t, original.GetClusterName(), updated.GetClusterName())
			oldSecurity := original.ControlPlane().Machine().Security()
			newSecurity := updated.ControlPlane().Machine().Security()
			assert.True(
				t,
				bytes.Equal(oldSecurity.IssuingCA().Crt, newSecurity.IssuingCA().Crt),
				"machine CA changed",
			)
			assert.True(t, bytes.Equal(original.ControlPlane().Cluster().IssuingCA().Crt,
				updated.ControlPlane().Cluster().IssuingCA().Crt), "Kubernetes CA changed")
			assert.True(
				t,
				bytes.Equal([]byte(oldSecurity.Token()), []byte(newSecurity.Token())),
				"machine token changed",
			)
		})
	}
}

func loadTalosNetworkIdentityFixture(
	t *testing.T,
	contract *talosconfig.VersionContract,
) *talos.Configs {
	t.Helper()

	manager := talos.NewConfigManager(t.TempDir(), "network-test", "1.35.0", "10.5.0.0/24").
		WithVersionContract(contract)
	original, err := manager.Load(configmanager.LoadOptions{})
	require.NoError(t, err)

	return original
}

func TestConfigs_WithKubernetesNetworkRejectsInvalidCIDRs(t *testing.T) {
	t.Parallel()

	original, err := talos.NewDefaultConfigs()
	require.NoError(t, err)

	for _, pair := range [][2]string{
		{"", "10.128.0.0/16"},
		{"10.64.0.0/16", ""},
		{"not-a-prefix", "10.128.0.0/16"},
		{"10.64.0.0/16", "10.128.0.0/16\nextra: value"},
	} {
		updated, err := original.WithKubernetesNetwork(pair[0], pair[1])
		require.Error(t, err)
		assert.Nil(t, updated)
	}
}
