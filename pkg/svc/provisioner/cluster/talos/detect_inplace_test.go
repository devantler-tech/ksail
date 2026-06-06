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
// sections (registry mirrors + cert SANs) are copied from running into desired,
// while desired's own content is preserved — the mirror-reversion fix.
func TestGraftNodeManagedSections(t *testing.T) {
	t.Parallel()

	running := wrapConfig(&v1alpha1.Config{
		MachineConfig: &v1alpha1.MachineConfig{
			MachineRegistries: v1alpha1.RegistriesConfig{
				RegistryMirrors: map[string]*v1alpha1.RegistryMirrorConfig{
					"docker.io": {MirrorEndpoints: []string{mirrorEndpoint}},
				},
			},
			MachineCertSANs: []string{"injected.example.com"},
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

	diff, err := talosprovisioner.MachineConfigDiffForTest(running, desired)
	require.NoError(t, err)
	assert.Empty(t, diff,
		"a create-injected hostname must not read as drift on an unchanged config")
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
