package talosprovisioner_test

import (
	"io"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	configmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager"
	ksailconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/ksail"
	talosconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/talos"
	talosprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/talos"
	"github.com/siderolabs/talos/pkg/machinery/config/generate/secrets"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	renderFixtureDir     = "testdata/render-hetzner"
	renderFixtureCluster = "prod"
	renderEndpointIP     = "203.0.113.10"
)

func renderCertSANs() []string {
	return []string{renderEndpointIP, "198.51.100.1", "198.51.100.2"}
}

// loadRenderFixture loads the Talos/Hetzner fixture through KSail's own config
// manager — the same loader cluster create uses — so the Hetzner runtime
// patches, extensions and kernel-argument fold are all in play.
func loadRenderFixture(t *testing.T) (*v1alpha1.Cluster, *talosconfigmanager.Configs) {
	t.Helper()

	t.Chdir(renderFixtureDir)

	cfgManager := ksailconfigmanager.NewConfigManager(io.Discard, "ksail.yaml")

	cluster, err := cfgManager.Load(configmanager.LoadOptions{Silent: true})
	require.NoError(t, err)
	require.NotNil(t, cfgManager.DistributionConfig.Talos)

	return cluster, cfgManager.DistributionConfig.Talos
}

// TestRenderMachineConfigs_EqualsHetznerCreateApply asserts that the offline
// render equals, byte for byte, the configuration cluster create applies to
// the first control-plane and first worker node on Hetzner with the floating
// IP enabled.
func TestRenderMachineConfigs_EqualsHetznerCreateApply(t *testing.T) {
	// The create path reads the token from the environment; give it the value
	// the render embeds so the comparison covers the VIP block too.
	t.Setenv(v1alpha1.DefaultHetznerTokenEnvVar, talosprovisioner.RenderPlaceholderHcloudToken)

	cluster, configs := loadRenderFixture(t)

	rendered, err := talosprovisioner.RenderMachineConfigs(
		configs,
		cluster.Spec.Cluster.Provider,
		cluster.Spec.Provider.Hetzner,
		talosprovisioner.RenderOptions{EndpointIP: renderEndpointIP, CertSANs: renderCertSANs()},
	)
	require.NoError(t, err)

	prov := talosprovisioner.NewProvisioner(configs, nil).
		WithHetznerOptions(cluster.Spec.Provider.Hetzner).
		WithLogWriter(io.Discard)

	createCP, createWorker, err := prov.HetznerCreateNodeConfigsForTest(
		renderFixtureCluster, renderEndpointIP, renderCertSANs(),
	)
	require.NoError(t, err)

	assert.Equal(t, string(createCP), string(rendered.ControlPlane))
	assert.Equal(t, string(createWorker), string(rendered.Worker))
	assert.Equal(t, "prod-control-plane-1", rendered.ControlPlaneNode)
	assert.Equal(t, "prod-worker-1", rendered.WorkerNode)

	controlPlane := string(rendered.ControlPlane)

	// The post-installer kernel-argument fold is done: the arguments live in
	// the factory schematic, not in machine.install.extraKernelArgs.
	assert.NotContains(t, controlPlane, "extraKernelArgs")
	assert.Contains(t, controlPlane, "grubUseUKICmdline: true")
	assert.Contains(t, controlPlane, "factory.talos.dev/metal-installer/"+configs.SchematicID())
	assert.Contains(t, controlPlane, "https://"+renderEndpointIP+":6443")
	assert.Contains(t, controlPlane, "hostname: prod-control-plane-1")
	assert.Contains(t, controlPlane, talosprovisioner.RenderPlaceholderHcloudToken)
	assert.Contains(t, string(rendered.Worker), "vm.max_map_count")
}

// TestRenderMachineConfigs_NeverEmbedsTheEnvironmentToken asserts the render
// needs no credentials and never writes the real Hetzner token.
func TestRenderMachineConfigs_NeverEmbedsTheEnvironmentToken(t *testing.T) {
	const realToken = "real-hcloud-token-must-not-leak" //nolint:gosec // test sentinel, not a credential.

	t.Setenv(v1alpha1.DefaultHetznerTokenEnvVar, realToken)

	cluster, configs := loadRenderFixture(t)

	rendered, err := talosprovisioner.RenderMachineConfigs(
		configs, cluster.Spec.Cluster.Provider, cluster.Spec.Provider.Hetzner,
		talosprovisioner.RenderOptions{EndpointIP: renderEndpointIP},
	)
	require.NoError(t, err)

	assert.NotContains(t, string(rendered.ControlPlane), realToken)
	assert.Contains(t, string(rendered.ControlPlane), talosprovisioner.RenderPlaceholderHcloudToken)
}

// TestRenderMachineConfigs_SuppliedSecretsAreDeterministic asserts that a
// supplied secrets bundle replaces the generated PKI and makes the render
// reproducible.
//
//nolint:paralleltest // changes the working directory.
func TestRenderMachineConfigs_SuppliedSecretsAreDeterministic(t *testing.T) {
	cluster, configs := loadRenderFixture(t)

	bundle, err := configs.ExtractSecrets()
	require.NoError(t, err)

	fresh, err := talosconfigmanager.NewDefaultConfigs()
	require.NoError(t, err)

	otherBundle, err := fresh.ExtractSecrets()
	require.NoError(t, err)

	render := func(b *secrets.Bundle) *talosprovisioner.RenderedMachineConfigs {
		t.Helper()

		out, renderErr := talosprovisioner.RenderMachineConfigs(
			configs, cluster.Spec.Cluster.Provider, cluster.Spec.Provider.Hetzner,
			talosprovisioner.RenderOptions{EndpointIP: renderEndpointIP, Secrets: b},
		)
		require.NoError(t, renderErr)

		return out
	}

	first, second := render(otherBundle), render(otherBundle)
	assert.Equal(t, string(first.ControlPlane), string(second.ControlPlane))
	assert.Equal(t, string(first.Worker), string(second.Worker))

	own := render(bundle)
	assert.NotEqual(t, string(own.ControlPlane), string(first.ControlPlane))
	assert.Contains(t, string(first.ControlPlane), otherBundle.Secrets.BootstrapToken)
}

func TestRenderMachineConfigs_RejectsOtherProviders(t *testing.T) {
	t.Parallel()

	configs, err := talosconfigmanager.NewDefaultConfigs()
	require.NoError(t, err)

	_, err = talosprovisioner.RenderMachineConfigs(
		configs, v1alpha1.ProviderDocker, v1alpha1.OptionsHetzner{},
		talosprovisioner.RenderOptions{},
	)
	require.ErrorIs(t, err, talosprovisioner.ErrRenderProviderUnsupported)
}
