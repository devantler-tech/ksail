package talosprovisioner_test

import (
	"testing"

	talosconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/talos"
	talosprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/talos"
	talosconfig "github.com/siderolabs/talos/pkg/machinery/config"
	"github.com/siderolabs/talos/pkg/machinery/config/configloader"
	taloscontainer "github.com/siderolabs/talos/pkg/machinery/config/container"
	"github.com/siderolabs/talos/pkg/machinery/config/types/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	sysctlRmemMax  = "net.core.rmem_max"
	sysctlKptr     = "kernel.kptr_restrict"
	mirrorEndpoint = "http://talos-default-docker.io:5000"
)

// wrapConfig wraps a v1alpha1.Config as a full Talos config provider.
func wrapConfig(cfg *v1alpha1.Config) talosconfig.Provider {
	return taloscontainer.NewV1Alpha1(cfg)
}

// sysctlsConfig builds a provider carrying the given sysctls.
func sysctlsConfig(sysctls map[string]string) talosconfig.Provider {
	return wrapConfig(&v1alpha1.Config{
		MachineConfig: &v1alpha1.MachineConfig{MachineSysctls: sysctls},
	})
}

// sysctlPatch builds a cluster-scoped user patch from raw YAML content.
func sysctlPatch(content string) talosconfigmanager.Patch {
	return talosconfigmanager.Patch{
		Path:    "test-patch.yaml",
		Scope:   talosconfigmanager.PatchScopeCluster,
		Content: []byte(content),
	}
}

// runningFromPatches generates a config from patches and parses it back, as the
// node would store and return it.
func runningFromPatches(t *testing.T, patches ...talosconfigmanager.Patch) talosconfig.Provider {
	t.Helper()

	configs, err := talosconfigmanager.NewDefaultConfigsWithPatches(patches)
	require.NoError(t, err)

	configBytes, err := configs.ControlPlane().Bytes()
	require.NoError(t, err)

	running, err := configloader.NewFromBytes(configBytes)
	require.NoError(t, err)

	return running
}

func TestMachineConfigDiff(t *testing.T) {
	t.Parallel()

	base := sysctlsConfig(map[string]string{sysctlRmemMax: "1"})

	identical, err := talosprovisioner.MachineConfigDiffForTest(
		base, sysctlsConfig(map[string]string{sysctlRmemMax: "1"}),
	)
	require.NoError(t, err)
	assert.Empty(t, identical, "identical configs produce no diff")

	differ, err := talosprovisioner.MachineConfigDiffForTest(
		base, sysctlsConfig(map[string]string{sysctlRmemMax: "2"}),
	)
	require.NoError(t, err)
	assert.NotEmpty(t, differ, "differing configs produce a diff")
}

// TestGraftNodeManagedSections verifies the create-time-injected node-managed
// sections (registry mirrors, cert SANs, and HCloud VIP) are copied from running
// into desired, while desired's own content is preserved.
//
//nolint:staticcheck // test fixture exercises the active Talos v1alpha1 config API
func TestGraftNodeManagedSections(t *testing.T) {
	t.Parallel()

	dhcp := true

	running := wrapConfig(&v1alpha1.Config{
		MachineConfig: &v1alpha1.MachineConfig{
			MachineRegistries: v1alpha1.RegistriesConfig{
				RegistryMirrors: map[string]*v1alpha1.RegistryMirrorConfig{
					"docker.io": {MirrorEndpoints: []string{mirrorEndpoint}},
				},
			},
			MachineCertSANs: []string{"injected.example.com"},
			MachineNetwork: &v1alpha1.NetworkConfig{
				NetworkInterfaces: v1alpha1.NetworkDeviceList{
					{
						DeviceInterface: "eth0",
						DeviceAddresses: []string{"203.0.113.5/32"},
						DeviceMTU:       9000,
						DeviceDHCP:      &dhcp,
						DeviceVIPConfig: &v1alpha1.DeviceVIPConfig{
							SharedIP: "192.0.2.10",
							HCloudConfig: &v1alpha1.VIPHCloudConfig{
								HCloudAPIToken: "test-token",
							},
						},
					},
				},
			},
		},
	})
	desired := sysctlsConfig(map[string]string{sysctlRmemMax: "1"})

	grafted, err := talosprovisioner.GraftNodeManagedSectionsForTest(desired, running)
	require.NoError(t, err)

	graftedBytes, err := grafted.Bytes()
	require.NoError(t, err)

	assert.Contains(t, string(graftedBytes), mirrorEndpoint, "mirrors grafted from running")
	assert.Contains(
		t,
		string(graftedBytes),
		"injected.example.com",
		"certSANs grafted from running",
	)
	assert.Contains(t, string(graftedBytes), sysctlRmemMax, "desired's own content preserved")

	devices := grafted.Machine().Network().Devices()
	require.Len(t, devices, 1)
	require.NotNil(t, devices[0].VIPConfig())
	assert.Equal(t, "192.0.2.10", devices[0].VIPConfig().IP())
	require.NotNil(t, devices[0].VIPConfig().HCloud())

	graftedDevice := grafted.RawV1Alpha1().MachineConfig.MachineNetwork.NetworkInterfaces[0]
	assert.Empty(t, graftedDevice.DeviceAddresses,
		"unmanaged runtime addresses must remain visible as removable drift")
	assert.Zero(t, graftedDevice.DeviceMTU,
		"unmanaged runtime MTU must remain visible as removable drift")
	require.NotNil(t, graftedDevice.DeviceDHCP)
	assert.True(t, *graftedDevice.DeviceDHCP, "the VIP interface must retain its DHCP prerequisite")
}

// TestGraftNodeManagedSections_PreservesDesiredHCloudVIP verifies that an
// explicit reconcile target wins over stale runtime VIP state.
//
//nolint:staticcheck // test fixture exercises the active Talos v1alpha1 config API
func TestGraftNodeManagedSections_PreservesDesiredHCloudVIP(t *testing.T) {
	t.Parallel()

	configWithVIP := func(address, token string) talosconfig.Provider {
		return wrapConfig(&v1alpha1.Config{
			MachineConfig: &v1alpha1.MachineConfig{
				MachineNetwork: &v1alpha1.NetworkConfig{
					NetworkInterfaces: v1alpha1.NetworkDeviceList{
						{
							DeviceInterface: "eth0",
							DeviceVIPConfig: &v1alpha1.DeviceVIPConfig{
								SharedIP: address,
								HCloudConfig: &v1alpha1.VIPHCloudConfig{
									HCloudAPIToken: token,
								},
							},
						},
					},
				},
			},
		})
	}

	grafted, err := talosprovisioner.GraftNodeManagedSectionsForTest(
		configWithVIP("192.0.2.20", "new-token"),
		configWithVIP("192.0.2.10", "old-token"),
	)
	require.NoError(t, err)

	devices := grafted.Machine().Network().Devices()
	require.Len(t, devices, 1)
	vip := devices[0].VIPConfig()
	require.NotNil(t, vip)
	assert.Equal(t, "192.0.2.20", vip.IP())
	require.NotNil(t, vip.HCloud())
	assert.Equal(t, "new-token", vip.HCloud().APIToken())
}

// TestBuildDesiredNodeConfig_NoFalsePositive is the no-false-positive guard: an
// unchanged config (same patches) reports no drift after alignment + graft.
func TestBuildDesiredNodeConfig_NoFalsePositive(t *testing.T) {
	t.Parallel()

	patch := sysctlPatch("machine:\n  sysctls:\n    net.core.rmem_max: \"1\"\n")
	running := runningFromPatches(t, patch)

	configs, err := talosconfigmanager.NewDefaultConfigsWithPatches(
		[]talosconfigmanager.Patch{patch},
	)
	require.NoError(t, err)

	prov := talosprovisioner.NewProvisioner(configs, nil)

	desired, err := prov.BuildDesiredNodeConfigForTest(
		running, running, talosprovisioner.RoleControlPlane,
	)
	require.NoError(t, err)

	diff, err := talosprovisioner.MachineConfigDiffForTest(running, desired)
	require.NoError(t, err)
	assert.Empty(t, diff, "an unchanged config must not report drift")
}

// TestBuildDesiredNodeConfig_PreservesCreateInjectedMirrors reproduces the Docker
// system-test scenario: the running config carries registry mirrors injected at
// create (which a regenerate lacks). An unchanged config must still report no
// drift because the mirrors are grafted from running.
func TestBuildDesiredNodeConfig_PreservesCreateInjectedMirrors(t *testing.T) {
	t.Parallel()

	patch := sysctlPatch("machine:\n  sysctls:\n    net.core.rmem_max: \"1\"\n")

	runningConfigs, err := talosconfigmanager.NewDefaultConfigsWithPatches(
		[]talosconfigmanager.Patch{patch},
	)
	require.NoError(t, err)

	// Mirrors are injected at create only (ApplyMirrorRegistries), like the DinD flow.
	err = runningConfigs.ApplyMirrorRegistries([]talosconfigmanager.MirrorRegistry{
		{Host: "docker.io", Endpoints: []string{mirrorEndpoint}},
	})
	require.NoError(t, err)

	runningBytes, err := runningConfigs.ControlPlane().Bytes()
	require.NoError(t, err)

	running, err := configloader.NewFromBytes(runningBytes)
	require.NoError(t, err)

	// Desired configs are regenerated without mirrors (create-only transform).
	desiredConfigs, err := talosconfigmanager.NewDefaultConfigsWithPatches(
		[]talosconfigmanager.Patch{patch},
	)
	require.NoError(t, err)

	prov := talosprovisioner.NewProvisioner(desiredConfigs, nil)

	desired, err := prov.BuildDesiredNodeConfigForTest(
		running, running, talosprovisioner.RoleControlPlane,
	)
	require.NoError(t, err)

	diff, err := talosprovisioner.MachineConfigDiffForTest(running, desired)
	require.NoError(t, err)
	assert.Empty(t, diff, "create-injected mirrors must not read as drift on an unchanged config")
}

// TestBuildDesiredNodeConfig_PreservesRuntimeHetznerVIP verifies that a fresh
// CLI invocation does not detect or apply removal of the runtime-injected
// HCloud VIP endpoint from an otherwise unchanged control plane.
func TestBuildDesiredNodeConfig_PreservesRuntimeHetznerVIP(t *testing.T) {
	t.Parallel()

	runningConfigs, err := talosconfigmanager.NewDefaultConfigsWithPatches(nil)
	require.NoError(t, err)
	runningConfigs, err = runningConfigs.WithEndpoint("192.0.2.10")
	require.NoError(t, err)
	runningConfigs, err = runningConfigs.WithHetznerVIP("192.0.2.10", "test-token")
	require.NoError(t, err)

	runningBytes, err := runningConfigs.ControlPlane().Bytes()
	require.NoError(t, err)
	running, err := configloader.NewFromBytes(runningBytes)
	require.NoError(t, err)

	desiredConfigs, err := talosconfigmanager.NewDefaultConfigsWithPatches(nil)
	require.NoError(t, err)

	provisioner := talosprovisioner.NewProvisioner(desiredConfigs, nil)

	desired, err := provisioner.BuildDesiredNodeConfigForTest(
		running, running, talosprovisioner.RoleControlPlane,
	)
	require.NoError(t, err)

	diff, err := talosprovisioner.MachineConfigDiffForTest(running, desired)
	require.NoError(t, err)
	assert.Empty(t, diff, "runtime HCloud VIP must not read as removable config drift")
}

// TestBuildDesiredNodeConfig_DetectsRemoval is the key test for the fix: a sysctl
// removed from the patch files must be detected as drift (and thus removed on apply).
func TestBuildDesiredNodeConfig_DetectsRemoval(t *testing.T) {
	t.Parallel()

	// Cluster was created with two sysctls.
	running := runningFromPatches(t, sysctlPatch(
		"machine:\n  sysctls:\n    net.core.rmem_max: \"1\"\n    kernel.kptr_restrict: \"2\"\n",
	))

	// The patch now sets only one sysctl — kernel.kptr_restrict was removed.
	desiredConfigs, err := talosconfigmanager.NewDefaultConfigsWithPatches(
		[]talosconfigmanager.Patch{
			sysctlPatch("machine:\n  sysctls:\n    net.core.rmem_max: \"1\"\n"),
		},
	)
	require.NoError(t, err)

	prov := talosprovisioner.NewProvisioner(desiredConfigs, nil)

	desired, err := prov.BuildDesiredNodeConfigForTest(
		running, running, talosprovisioner.RoleControlPlane,
	)
	require.NoError(t, err)

	diff, err := talosprovisioner.MachineConfigDiffForTest(running, desired)
	require.NoError(t, err)
	assert.NotEmpty(t, diff, "removing a sysctl from a patch must be detected as drift")
	assert.Contains(t, diff, sysctlKptr, "the removed sysctl key must appear in the diff")
}

// runningAtKubernetesVersion renders a control-plane config at the given
// Kubernetes version and parses it back, as a node would store and return it.
func runningAtKubernetesVersion(t *testing.T, version string) talosconfig.Provider {
	t.Helper()

	configs, err := talosconfigmanager.NewDefaultConfigsWithPatches(nil)
	require.NoError(t, err)

	configs, err = configs.WithKubernetesVersion(version)
	require.NoError(t, err)

	configBytes, err := configs.ControlPlane().Bytes()
	require.NoError(t, err)

	running, err := configloader.NewFromBytes(configBytes)
	require.NoError(t, err)

	return running
}

// TestBuildDesiredNodeConfig_PreservesRunningKubernetesVersion is the core
// regression guard for issue #4936: when no Kubernetes version is pinned, an
// update must render the desired config at the version already running on the
// cluster (here older than KSail's built-in default), so it reports no drift and
// never proposes an unrequested — possibly Talos-incompatible — upgrade.
func TestBuildDesiredNodeConfig_PreservesRunningKubernetesVersion(t *testing.T) {
	t.Parallel()

	// Cluster is running an older Kubernetes version than the built-in default.
	running := runningAtKubernetesVersion(t, "1.32.0")

	// Desired configs use the built-in default (e.g. 1.36.0); no version pinned.
	desiredConfigs, err := talosconfigmanager.NewDefaultConfigsWithPatches(nil)
	require.NoError(t, err)
	require.NotEqual(t, "1.32.0", desiredConfigs.KubernetesVersion(),
		"precondition: default must differ from the running version")

	prov := talosprovisioner.NewProvisioner(desiredConfigs, nil) // nil options => unpinned

	desired, err := prov.BuildDesiredNodeConfigForTest(
		running, running, talosprovisioner.RoleControlPlane,
	)
	require.NoError(t, err)

	diff, err := talosprovisioner.MachineConfigDiffForTest(running, desired)
	require.NoError(t, err)
	assert.Empty(t, diff,
		"unpinned update must track the running Kubernetes version (no unrequested upgrade)")
}

// TestBuildDesiredNodeConfig_PinnedKubernetesVersionAppliesUpgrade verifies the
// other half: when a Kubernetes version IS pinned, the desired config keeps that
// version, so an intentional change relative to the running cluster is detected.
func TestBuildDesiredNodeConfig_PinnedKubernetesVersionAppliesUpgrade(t *testing.T) {
	t.Parallel()

	running := runningAtKubernetesVersion(t, "1.32.0")

	// Desired configs are built at a different version, and the user pinned it.
	desiredConfigs, err := talosconfigmanager.NewDefaultConfigsWithPatches(nil)
	require.NoError(t, err)

	desiredConfigs, err = desiredConfigs.WithKubernetesVersion("1.33.0")
	require.NoError(t, err)

	options := talosprovisioner.NewOptions().WithKubernetesVersion("1.33.0")
	prov := talosprovisioner.NewProvisioner(desiredConfigs, options)

	desired, err := prov.BuildDesiredNodeConfigForTest(
		running, running, talosprovisioner.RoleControlPlane,
	)
	require.NoError(t, err)

	diff, err := talosprovisioner.MachineConfigDiffForTest(running, desired)
	require.NoError(t, err)
	assert.NotEmpty(t, diff, "a pinned version differing from running must be detected")
}

// runningWithHostname renders a config for the given role and injects the static
// machine.network.hostname the same way create does on Hetzner (PatchTalosHostname:
// set machine.network.hostname, strip the conflicting standalone HostnameConfig
// document), then parses it back as a node would store and return it.
func runningWithHostname(
	t *testing.T,
	role, hostname string,
	patches ...talosconfigmanager.Patch,
) talosconfig.Provider {
	t.Helper()

	configs, err := talosconfigmanager.NewDefaultConfigsWithPatches(patches)
	require.NoError(t, err)

	roleConfig := configs.ControlPlane()
	if role == talosprovisioner.RoleWorker {
		roleConfig = configs.Worker()
	}

	roleBytes, err := roleConfig.Bytes()
	require.NoError(t, err)

	withHostname, err := talosprovisioner.PatchTalosHostname(roleBytes, hostname)
	require.NoError(t, err)

	running, err := configloader.NewFromBytes(withHostname)
	require.NoError(t, err)

	return running
}

// TestBuildDesiredNodeConfig_PreservesNodeHostname is the regression guard for the
// node-rename bug: on Hetzner the per-node static hostname (machine.network.hostname)
// is injected post-generation at create (PatchTalosHostname) so the Hetzner CCM can
// match the Kubernetes Node to its server. It is neither in the base config nor in
// any user patch, so a freshly regenerated desired config omits it and instead
// carries the SDK's default standalone HostnameConfig document (auto: stable).
// Without grafting the running hostname, an update (a) surfaces the hostname as
// phantom drift and (b) strips the static hostname when applied, so the node
// re-registers under a generated talos-xxxxx name on its next reboot (e.g. during a
// Talos OS upgrade via `ksail cluster update`).
func TestBuildDesiredNodeConfig_PreservesNodeHostname(t *testing.T) {
	t.Parallel()

	patch := sysctlPatch("machine:\n  sysctls:\n    net.core.rmem_max: \"1\"\n")
	running := runningWithHostname(
		t, talosprovisioner.RoleControlPlane, "prod-control-plane-1", patch,
	)

	require.Equal(t, "prod-control-plane-1", running.RawV1Alpha1().Hostname(),
		"precondition: running config carries the create-injected static hostname")

	// Desired configs are regenerated from the same patches — without the hostname,
	// which is a create-only post-generation transform (like registry mirrors).
	desiredConfigs, err := talosconfigmanager.NewDefaultConfigsWithPatches(
		[]talosconfigmanager.Patch{patch},
	)
	require.NoError(t, err)

	prov := talosprovisioner.NewProvisioner(desiredConfigs, nil)

	desired, err := prov.BuildDesiredNodeConfigForTest(
		running, running, talosprovisioner.RoleControlPlane,
	)
	require.NoError(t, err)

	assert.Equal(t, "prod-control-plane-1", desired.RawV1Alpha1().Hostname(),
		"the per-node static hostname must be preserved so the node keeps its name on reboot")

	// The fix hinges on stripping the conflicting standalone HostnameConfig
	// document: the grafted config must carry the static hostname in exactly one
	// representation, so the node accepts it (no "static hostname is already set"
	// conflict, #4969) on ApplyConfiguration.
	desiredBytes, err := desired.Bytes()
	require.NoError(t, err)
	assert.Zero(t, countHostnameConfigDocs(t, desiredBytes),
		"the conflicting HostnameConfig document must be stripped after grafting")

	_, err = desired.ValidateAsClient(nodeRuntimeMode{})
	require.NoError(t, err,
		"grafted config must pass node-side validation (no static-vs-HostnameConfig conflict)")

	diff, err := talosprovisioner.MachineConfigDiffForTest(running, desired)
	require.NoError(t, err)
	assert.Empty(t, diff,
		"a create-injected hostname must not read as drift on an unchanged config")
}

// TestBuildDesiredNodeConfig_PreservesNodeHostname_Worker is the worker-role
// counterpart: the rename bug affects workers too. On Hetzner, workers also get a
// create-injected static hostname (marshalConfigWithHostname is applied to worker
// servers), and applyInPlaceConfigChanges rebuilds each worker's config via
// buildDesiredNodeConfig. A worker's own config carries no CA private keys, so the
// rebuild is seeded from a control-plane secrets source (#4963) — exactly how the
// apply path threads it.
func TestBuildDesiredNodeConfig_PreservesNodeHostname_Worker(t *testing.T) {
	t.Parallel()

	patch := sysctlPatch("machine:\n  sysctls:\n    net.core.rmem_max: \"1\"\n")

	// One cluster bundle backs the running worker, the control-plane secrets source,
	// and the provisioner's desired configs — modelling reality, where every node
	// shares the cluster PKI. (Distinct bundles would diff on CA certs, not hostname.)
	configs, err := talosconfigmanager.NewDefaultConfigsWithPatches(
		[]talosconfigmanager.Patch{patch},
	)
	require.NoError(t, err)

	// The worker's running config carries the create-injected static hostname.
	workerBytes, err := configs.Worker().Bytes()
	require.NoError(t, err)

	workerWithHostname, err := talosprovisioner.PatchTalosHostname(workerBytes, "prod-worker-1")
	require.NoError(t, err)

	workerRunning, err := configloader.NewFromBytes(workerWithHostname)
	require.NoError(t, err)

	require.Equal(t, "prod-worker-1", workerRunning.RawV1Alpha1().Hostname(),
		"precondition: running worker config carries the create-injected static hostname")

	// Workers carry no CA private keys; the rebuild is seeded from a control-plane
	// config of the same cluster (matching PKI), as the apply path threads it (#4963).
	cpBytes, err := configs.ControlPlane().Bytes()
	require.NoError(t, err)

	cpSecretsSource, err := configloader.NewFromBytes(cpBytes)
	require.NoError(t, err)

	prov := talosprovisioner.NewProvisioner(configs, nil)

	desired, err := prov.BuildDesiredNodeConfigForTest(
		workerRunning, cpSecretsSource, talosprovisioner.RoleWorker,
	)
	require.NoError(t, err)

	assert.Equal(t, "prod-worker-1", desired.RawV1Alpha1().Hostname(),
		"the worker's per-node static hostname must be preserved so it keeps its name on reboot")

	desiredBytes, err := desired.Bytes()
	require.NoError(t, err)
	assert.Zero(
		t,
		countHostnameConfigDocs(t, desiredBytes),
		"the conflicting HostnameConfig document must be stripped after grafting the worker hostname",
	)

	_, err = desired.ValidateAsClient(nodeRuntimeMode{})
	require.NoError(
		t,
		err,
		"grafted worker config must pass node-side validation (no static-vs-HostnameConfig conflict)",
	)

	// Scope the drift check to the hostname: the empty-diff guarantee is covered by
	// the control-plane test. A worker config carries no apiserver image, so
	// buildDesiredNodeConfig cannot realign its Kubernetes version (see
	// alignKubernetesVersion / #4936) — an orthogonal, pre-existing difference that
	// must not mask, nor be conflated with, the hostname assertion.
	diff, err := talosprovisioner.MachineConfigDiffForTest(workerRunning, desired)
	require.NoError(t, err)
	assert.NotContains(t, diff, "HostnameConfig",
		"the grafted worker config must not reintroduce a standalone HostnameConfig document")
	assert.NotContains(t, diff, "hostname:",
		"the worker's static hostname must not drift (no add/remove of machine.network.hostname)")
}

// TestBuildDesiredNodeConfig_NoHostnameLeavesConfigUnchanged verifies the Docker
// path is unaffected: a node with no static machine.network.hostname (it derives
// its hostname from the container) keeps the regenerated config's own hostname
// representation, so grafting is a no-op and reports no drift.
func TestBuildDesiredNodeConfig_NoHostnameLeavesConfigUnchanged(t *testing.T) {
	t.Parallel()

	patch := sysctlPatch("machine:\n  sysctls:\n    net.core.rmem_max: \"1\"\n")
	running := runningFromPatches(t, patch)

	require.Empty(t, running.RawV1Alpha1().Hostname(),
		"precondition: a non-Hetzner node carries no static hostname")

	desiredConfigs, err := talosconfigmanager.NewDefaultConfigsWithPatches(
		[]talosconfigmanager.Patch{patch},
	)
	require.NoError(t, err)

	prov := talosprovisioner.NewProvisioner(desiredConfigs, nil)

	desired, err := prov.BuildDesiredNodeConfigForTest(
		running, running, talosprovisioner.RoleControlPlane,
	)
	require.NoError(t, err)

	diff, err := talosprovisioner.MachineConfigDiffForTest(running, desired)
	require.NoError(t, err)
	assert.Empty(t, diff, "a node without a static hostname must not report drift")
}

func TestConfigFingerprint(t *testing.T) {
	t.Parallel()

	running := sysctlsConfig(map[string]string{sysctlRmemMax: "1"})
	fingerprint := talosprovisioner.ConfigFingerprintForTest(running)

	assert.Len(t, fingerprint, 12, "fingerprint length is fixed")
	assert.Equal(
		t,
		fingerprint,
		talosprovisioner.ConfigFingerprintForTest(
			sysctlsConfig(map[string]string{sysctlRmemMax: "1"}),
		),
		"fingerprint is deterministic for equal configs",
	)
	assert.NotEqual(
		t,
		fingerprint,
		talosprovisioner.ConfigFingerprintForTest(
			sysctlsConfig(map[string]string{sysctlRmemMax: "2"}),
		),
		"different configs produce different fingerprints",
	)
}

// workerRunningFromPatches generates a config and returns the WORKER side parsed
// back, as an existing worker node would store and return it. Unlike a
// control-plane config it carries the cluster CA certificate but no CA private
// keys — the shape that triggered #4963.
func workerRunningFromPatches(
	t *testing.T,
	patches ...talosconfigmanager.Patch,
) talosconfig.Provider {
	t.Helper()

	configs, err := talosconfigmanager.NewDefaultConfigsWithPatches(patches)
	require.NoError(t, err)

	workerBytes, err := configs.Worker().Bytes()
	require.NoError(t, err)

	running, err := configloader.NewFromBytes(workerBytes)
	require.NoError(t, err)

	return running
}

// TestBuildDesiredNodeConfig_WorkerUsesControlPlaneSecrets is the regression guard
// for issue #4963: rebuilding an existing worker's desired config must seed the
// cluster PKI from a control-plane config, because worker configs carry no CA
// private keys. Before the fix this failed with "align secrets for config
// comparison: failed to create config bundle: failed to parse PEM block", silently
// skipping in-place machine.config changes on every existing worker.
func TestBuildDesiredNodeConfig_WorkerUsesControlPlaneSecrets(t *testing.T) {
	t.Parallel()

	patch := sysctlPatch("machine:\n  sysctls:\n    net.core.rmem_max: \"1\"\n")

	configs, err := talosconfigmanager.NewDefaultConfigsWithPatches(
		[]talosconfigmanager.Patch{patch},
	)
	require.NoError(t, err)

	// running is the worker's own config (no CA private keys); the secrets source
	// is a control-plane config (full PKI) — exactly how the apply path threads it.
	workerBytes, err := configs.Worker().Bytes()
	require.NoError(t, err)

	workerRunning, err := configloader.NewFromBytes(workerBytes)
	require.NoError(t, err)

	cpBytes, err := configs.ControlPlane().Bytes()
	require.NoError(t, err)

	cpSecretsSource, err := configloader.NewFromBytes(cpBytes)
	require.NoError(t, err)

	prov := talosprovisioner.NewProvisioner(configs, nil)

	desired, err := prov.BuildDesiredNodeConfigForTest(
		workerRunning, cpSecretsSource, talosprovisioner.RoleWorker,
	)
	require.NoError(t, err,
		"worker desired-config build must succeed when seeded with control-plane PKI")

	desiredBytes, err := desired.Bytes()
	require.NoError(t, err)
	assert.Contains(t, string(desiredBytes), "type: worker",
		"the produced config is the worker config")
	assert.Contains(t, string(desiredBytes), sysctlRmemMax,
		"the patched sysctl reaches the worker config")
}

// TestBuildDesiredNodeConfig_WorkerSecretsSourceIsActionable verifies that seeding
// the rebuild from a worker config (no cluster CA private key) yields a clear,
// named error naming the control-plane PKI requirement, rather than Talos's opaque
// "failed to parse PEM block" (the issue's "Expected" clause).
func TestBuildDesiredNodeConfig_WorkerSecretsSourceIsActionable(t *testing.T) {
	t.Parallel()

	patch := sysctlPatch("machine:\n  sysctls:\n    net.core.rmem_max: \"1\"\n")
	workerRunning := workerRunningFromPatches(t, patch)

	configs, err := talosconfigmanager.NewDefaultConfigsWithPatches(
		[]talosconfigmanager.Patch{patch},
	)
	require.NoError(t, err)

	prov := talosprovisioner.NewProvisioner(configs, nil)

	// Worker config as both running and secrets source — the pre-fix bug shape.
	_, err = prov.BuildDesiredNodeConfigForTest(
		workerRunning, workerRunning, talosprovisioner.RoleWorker,
	)
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "failed to parse PEM block",
		"must not surface Talos's opaque PEM error")
	assert.Contains(t, err.Error(), "control-plane",
		"error names the control-plane PKI requirement")
}

// parseConfig round-trips a provider through its serialized bytes, as a node
// would store and return it over the Talos API.
func parseConfig(t *testing.T, provider talosconfig.Provider) talosconfig.Provider {
	t.Helper()

	configBytes, err := provider.Bytes()
	require.NoError(t, err)

	parsed, err := configloader.NewFromBytes(configBytes)
	require.NoError(t, err)

	return parsed
}

// workerSysctlPatch builds a worker-scoped user patch from raw YAML content. A
// worker-scoped patch only reaches the worker config (talos/workers/), never the
// control-plane config.
func workerSysctlPatch(content string) talosconfigmanager.Patch {
	return talosconfigmanager.Patch{
		Path:    "workers/test-patch.yaml",
		Scope:   talosconfigmanager.PatchScopeWorker,
		Content: []byte(content),
	}
}

// TestDetectMachineConfigDrift_WorkerScopedPatchOnlyDetectedAtWorkerRole is the
// regression guard for the bug where `ksail cluster update` never pushed
// worker-scoped Talos patch changes to worker nodes: drift detection inspected
// only a control-plane node, where worker patches never appear, so the change was
// invisible and the (role-correct) apply path never ran.
//
// It asserts both halves: a worker-only patch change is (a) NOT visible at the
// control-plane role — exactly why a CP-only check missed it — and (b) IS
// detected at the worker role, so the update now applies it to the correct nodes.
func TestDetectMachineConfigDrift_WorkerScopedPatchOnlyDetectedAtWorkerRole(t *testing.T) {
	t.Parallel()

	workerPatch := workerSysctlPatch("machine:\n  sysctls:\n    net.core.rmem_max: \"1\"\n")

	// The running cluster was created WITHOUT the worker patch.
	runningConfigs, err := talosconfigmanager.NewDefaultConfigsWithPatches(nil)
	require.NoError(t, err)

	cpRunning := parseConfig(t, runningConfigs.ControlPlane())
	workerRunning := parseConfig(t, runningConfigs.Worker())

	// The desired config now carries the new worker-scoped patch.
	desiredConfigs, err := talosconfigmanager.NewDefaultConfigsWithPatches(
		[]talosconfigmanager.Patch{workerPatch},
	)
	require.NoError(t, err)

	prov := talosprovisioner.NewProvisioner(desiredConfigs, nil)

	// (a) Control-plane role: a worker patch never touches the control-plane
	// config, so no drift is reported — the blind spot the old CP-only check had.
	cpChanges, err := prov.DetectRoleMachineConfigDriftForTest(
		cpRunning, cpRunning, talosprovisioner.RoleControlPlane,
	)
	require.NoError(t, err)
	assert.Empty(t, cpChanges, "a worker-scoped patch must not surface as control-plane drift")

	// (b) Worker role: the same change IS detected, seeded with control-plane PKI.
	workerChanges, err := prov.DetectRoleMachineConfigDriftForTest(
		workerRunning, cpRunning, talosprovisioner.RoleWorker,
	)
	require.NoError(t, err)
	require.Len(
		t,
		workerChanges,
		1,
		"a worker-scoped patch change must be detected at the worker role",
	)
	assert.Equal(t, talosprovisioner.MachineConfigField, workerChanges[0].Field)
	assert.Contains(t, workerChanges[0].Reason, "worker",
		"the change reason names the worker role for operator clarity")
}

// TestDetectMachineConfigDrift_CleanWorkerNoDrift is the no-false-positive guard
// for worker drift detection: when the running worker already reflects the
// desired worker patches, the worker role reports no drift — so an unrelated
// update neither needlessly re-pushes nor churns worker configs.
func TestDetectMachineConfigDrift_CleanWorkerNoDrift(t *testing.T) {
	t.Parallel()

	workerPatch := workerSysctlPatch("machine:\n  sysctls:\n    net.core.rmem_max: \"1\"\n")

	configs, err := talosconfigmanager.NewDefaultConfigsWithPatches(
		[]talosconfigmanager.Patch{workerPatch},
	)
	require.NoError(t, err)

	workerRunning := parseConfig(t, configs.Worker())
	cpSecretsSource := parseConfig(t, configs.ControlPlane())

	prov := talosprovisioner.NewProvisioner(configs, nil)

	changes, err := prov.DetectRoleMachineConfigDriftForTest(
		workerRunning, cpSecretsSource, talosprovisioner.RoleWorker,
	)
	require.NoError(t, err)
	assert.Empty(t, changes, "an unchanged worker config must not report drift")
}

// TestDetectMachineConfigDrift_WorkerTracksRunningKubernetesVersion guards the
// worker analogue of #4936: when the cluster runs an older Kubernetes version
// than KSail's built-in default and no version is pinned, worker drift detection
// must track the running version (read from the kubelet image) rather than
// proposing an unrequested worker kubelet upgrade.
func TestDetectMachineConfigDrift_WorkerTracksRunningKubernetesVersion(t *testing.T) {
	t.Parallel()

	// Running cluster is at an older Kubernetes version than the built-in default.
	runningConfigs, err := talosconfigmanager.NewDefaultConfigsWithPatches(nil)
	require.NoError(t, err)

	runningConfigs, err = runningConfigs.WithKubernetesVersion("1.32.0")
	require.NoError(t, err)

	workerRunning := parseConfig(t, runningConfigs.Worker())
	cpSecretsSource := parseConfig(t, runningConfigs.ControlPlane())

	// Desired configs use the built-in default (newer); no version pinned.
	desiredConfigs, err := talosconfigmanager.NewDefaultConfigsWithPatches(nil)
	require.NoError(t, err)
	require.NotEqual(t, "1.32.0", desiredConfigs.KubernetesVersion(),
		"precondition: default must differ from the running version")

	prov := talosprovisioner.NewProvisioner(desiredConfigs, nil) // nil options => unpinned

	changes, err := prov.DetectRoleMachineConfigDriftForTest(
		workerRunning, cpSecretsSource, talosprovisioner.RoleWorker,
	)
	require.NoError(t, err)
	assert.Empty(t, changes,
		"unpinned update must track the running worker Kubernetes version (no unrequested upgrade)")
}
