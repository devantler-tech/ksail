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

	desired, err := prov.BuildDesiredNodeConfigForTest(running, talosprovisioner.RoleControlPlane)
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

	desired, err := prov.BuildDesiredNodeConfigForTest(running, talosprovisioner.RoleControlPlane)
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

	desired, err := prov.BuildDesiredNodeConfigForTest(running, talosprovisioner.RoleControlPlane)
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

	desired, err := prov.BuildDesiredNodeConfigForTest(running, talosprovisioner.RoleControlPlane)
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

	desired, err := prov.BuildDesiredNodeConfigForTest(running, talosprovisioner.RoleControlPlane)
	require.NoError(t, err)

	diff, err := talosprovisioner.MachineConfigDiffForTest(running, desired)
	require.NoError(t, err)
	assert.NotEmpty(t, diff, "a pinned version differing from running must be detected")
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
