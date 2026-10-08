package configmanager_test

import (
	"fmt"
	"io"
	"net/netip"
	"os"
	"path/filepath"
	"testing"

	configmanagerinterface "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager"
	configmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/ksail"
	talosmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/talos"
	talosconfig "github.com/siderolabs/talos/pkg/machinery/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The real loader must pass the provider's non-overlapping networks to both
// Talos node roles, whether the machine configuration is scaffolded or implicit.
func TestLoadConfig_NestedTalosUsesProviderNetworks(t *testing.T) {
	t.Parallel()

	for _, version := range []string{"v1.12.4", "v1.14.0-alpha.2"} {
		for _, scaffolded := range []bool{false, true} {
			for _, custom := range []bool{false, true} {
				t.Run(
					fmt.Sprintf("%s/scaffolded=%t/custom=%t", version, scaffolded, custom),
					func(t *testing.T) {
						t.Parallel()
						assertLoadedTalosNetworks(t, version, scaffolded, custom, "Kubernetes")
					},
				)
			}
		}
	}
}

func TestLoadConfig_DockerTalosKeepsDistributionNetworks(t *testing.T) {
	t.Parallel()
	assertLoadedTalosNetworks(t, "v1.14.0-alpha.2", false, true, "Docker")
}

func assertLoadedTalosNetworks(
	t *testing.T,
	version string,
	scaffolded, custom bool,
	provider string,
) {
	t.Helper()
	configs, wantPod, wantService := loadTalosNetworkFixture(
		t,
		version,
		scaffolded,
		custom,
		provider,
	)

	for _, nodeConfig := range []talosconfig.Config{configs.ControlPlane(), configs.Worker()} {
		network := nodeConfig.K8sNetworkConfig()
		require.NotNil(t, network)
		assert.Equal(t, []netip.Prefix{netip.MustParsePrefix(wantPod)}, network.PodCIDRs())
		assert.Equal(t, []netip.Prefix{netip.MustParsePrefix(wantService)}, network.ServiceCIDRs())
	}

	assert.Equal(
		t,
		wantPod,
		configs.NetworkCIDR(),
		"the CNI installer must receive the same pod network",
	)

	// Exposure rewrites performed during nested bootstrap must retain the networks.
	rewritten, err := configs.WithCertSANs([]string{"127.0.0.1", "192.0.2.2"})
	require.NoError(t, err)
	assert.Equal(
		t,
		[]netip.Prefix{netip.MustParsePrefix(wantPod)},
		rewritten.ControlPlane().K8sNetworkConfig().PodCIDRs(),
	)
	assert.Equal(
		t,
		[]netip.Prefix{netip.MustParsePrefix(wantService)},
		rewritten.Worker().K8sNetworkConfig().ServiceCIDRs(),
	)
}

func loadTalosNetworkFixture(
	t *testing.T,
	version string,
	scaffolded, custom bool,
	provider string,
) (*talosmanager.Configs, string, string) {
	t.Helper()

	dir := t.TempDir()
	patchesDir := filepath.Join(dir, "talos")

	if scaffolded {
		require.NoError(t, os.MkdirAll(patchesDir, 0o750))
	}

	providerConfig := ""
	wantPod, wantService := "10.64.0.0/16", "10.128.0.0/16"

	if custom {
		wantPod, wantService = "10.72.0.0/16", "10.136.0.0/16"
		providerConfig = "  provider:\n    kubernetes:\n      podCidr: 10.72.0.0/16\n      serviceCidr: 10.136.0.0/16\n"
	}

	if provider == "Docker" {
		wantPod, wantService = "10.244.0.0/16", "10.96.0.0/12"
	}

	configPath := filepath.Join(dir, "ksail.yaml")
	content := fmt.Sprintf(
		`apiVersion: ksail.io/v1alpha1
kind: Cluster
spec:
  cluster:
    distribution: Talos
    provider: %s
    distributionConfig: %q
    talos:
      version: %s
%s`,
		provider,
		patchesDir,
		version,
		providerConfig,
	)
	require.NoError(t, os.WriteFile(configPath, []byte(content), 0o600))

	manager := configmanager.NewConfigManager(io.Discard, "")
	manager.Viper.SetConfigFile(configPath)
	_, err := manager.Load(configmanagerinterface.LoadOptions{SkipValidation: true})
	require.NoError(t, err)
	require.NotNil(t, manager.DistributionConfig.Talos)

	return manager.DistributionConfig.Talos, wantPod, wantService
}
