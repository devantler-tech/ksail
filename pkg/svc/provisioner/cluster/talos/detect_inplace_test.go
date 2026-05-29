package talosprovisioner_test

import (
	"testing"

	talosconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/talos"
	talosprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/talos"
	talosconfig "github.com/siderolabs/talos/pkg/machinery/config"
	taloscontainer "github.com/siderolabs/talos/pkg/machinery/config/container"
	"github.com/siderolabs/talos/pkg/machinery/config/types/v1alpha1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const sysctlRmemMax = "net.core.rmem_max"

// wrapConfig wraps a v1alpha1.Config as a full Talos config provider.
func wrapConfig(cfg *v1alpha1.Config) talosconfig.Provider {
	return taloscontainer.NewV1Alpha1(cfg)
}

// sysctlsConfig builds a running-config provider carrying the given sysctls.
func sysctlsConfig(sysctls map[string]string) talosconfig.Provider {
	return wrapConfig(&v1alpha1.Config{
		MachineConfig: &v1alpha1.MachineConfig{MachineSysctls: sysctls},
	})
}

// clusterPatch builds a cluster-scoped user patch from raw YAML content.
func clusterPatch(content string) talosconfigmanager.Patch {
	return talosconfigmanager.Patch{
		Path:    "test-patch.yaml",
		Scope:   talosconfigmanager.PatchScopeCluster,
		Content: []byte(content),
	}
}

func TestMachineConfigDiff(t *testing.T) {
	t.Parallel()

	base := sysctlsConfig(map[string]string{sysctlRmemMax: "1"})

	identical, err := talosprovisioner.MachineConfigDiffForTest(
		base,
		sysctlsConfig(map[string]string{sysctlRmemMax: "1"}),
	)
	require.NoError(t, err)
	assert.Empty(t, identical, "identical configs produce no diff")

	differ, err := talosprovisioner.MachineConfigDiffForTest(
		base,
		sysctlsConfig(map[string]string{sysctlRmemMax: "2"}),
	)
	require.NoError(t, err)
	assert.NotEmpty(t, differ, "differing configs produce a diff")
}

// TestApplyUserPatches_DetectsAddedContent verifies a patch that adds a sysctl
// changes the config (the platform#1618 hardening case).
func TestApplyUserPatches_DetectsAddedContent(t *testing.T) {
	t.Parallel()

	running := sysctlsConfig(map[string]string{sysctlRmemMax: "1"})
	patch := clusterPatch("machine:\n  sysctls:\n    kernel.kptr_restrict: \"2\"\n")

	patched, err := talosprovisioner.ApplyUserPatchesForTest(
		running,
		[]talosconfigmanager.Patch{patch},
		talosprovisioner.RoleControlPlane,
	)
	require.NoError(t, err)

	diff, err := talosprovisioner.MachineConfigDiffForTest(running, patched)
	require.NoError(t, err)
	assert.NotEmpty(t, diff, "adding a sysctl via patch must be detected as drift")
}

// TestApplyUserPatches_NoDriftWhenAlreadyApplied is the no-false-positive guard:
// re-applying a patch already reflected in the running config yields no drift.
func TestApplyUserPatches_NoDriftWhenAlreadyApplied(t *testing.T) {
	t.Parallel()

	running := sysctlsConfig(map[string]string{sysctlRmemMax: "1"})
	patch := clusterPatch("machine:\n  sysctls:\n    net.core.rmem_max: \"1\"\n")

	patched, err := talosprovisioner.ApplyUserPatchesForTest(
		running,
		[]talosconfigmanager.Patch{patch},
		talosprovisioner.RoleControlPlane,
	)
	require.NoError(t, err)

	diff, err := talosprovisioner.MachineConfigDiffForTest(running, patched)
	require.NoError(t, err)
	assert.Empty(t, diff, "re-applying an already-present patch must not report drift")
}

// TestApplyUserPatches_PreservesRunningOnlyFields verifies a patch that doesn't
// mention registry mirrors leaves the running config's create-time-injected
// mirror endpoints intact — the mirror-reversion fix.
func TestApplyUserPatches_PreservesRunningOnlyFields(t *testing.T) {
	t.Parallel()

	const mirrorEndpoint = "http://talos-default-docker.io:5000"

	running := wrapConfig(&v1alpha1.Config{
		MachineConfig: &v1alpha1.MachineConfig{
			MachineSysctls: map[string]string{sysctlRmemMax: "1"},
			MachineRegistries: v1alpha1.RegistriesConfig{
				RegistryMirrors: map[string]*v1alpha1.RegistryMirrorConfig{
					"docker.io": {MirrorEndpoints: []string{mirrorEndpoint}},
				},
			},
		},
	})
	patch := clusterPatch("machine:\n  sysctls:\n    kernel.kptr_restrict: \"2\"\n")

	patched, err := talosprovisioner.ApplyUserPatchesForTest(
		running,
		[]talosconfigmanager.Patch{patch},
		talosprovisioner.RoleControlPlane,
	)
	require.NoError(t, err)

	patchedBytes, err := patched.Bytes()
	require.NoError(t, err)
	assert.Contains(
		t,
		string(patchedBytes),
		mirrorEndpoint,
		"patch must preserve the running config's mirror endpoints",
	)
}

// TestApplyUserPatches_RoleScoping verifies worker-scoped patches do not apply
// to a control-plane node.
func TestApplyUserPatches_RoleScoping(t *testing.T) {
	t.Parallel()

	running := sysctlsConfig(map[string]string{sysctlRmemMax: "1"})
	workerPatch := talosconfigmanager.Patch{
		Path:    "worker.yaml",
		Scope:   talosconfigmanager.PatchScopeWorker,
		Content: []byte("machine:\n  sysctls:\n    worker.only: \"1\"\n"),
	}

	patched, err := talosprovisioner.ApplyUserPatchesForTest(
		running,
		[]talosconfigmanager.Patch{workerPatch},
		talosprovisioner.RoleControlPlane,
	)
	require.NoError(t, err)

	diff, err := talosprovisioner.MachineConfigDiffForTest(running, patched)
	require.NoError(t, err)
	assert.Empty(t, diff, "worker-scoped patch must not apply to a control-plane node")
}

func TestPatchAppliesToRole(t *testing.T) {
	t.Parallel()

	assert.True(t, talosprovisioner.PatchAppliesToRoleForTest(
		talosconfigmanager.PatchScopeCluster, talosprovisioner.RoleControlPlane))
	assert.True(t, talosprovisioner.PatchAppliesToRoleForTest(
		talosconfigmanager.PatchScopeCluster, talosprovisioner.RoleWorker))
	assert.True(t, talosprovisioner.PatchAppliesToRoleForTest(
		talosconfigmanager.PatchScopeControlPlane, talosprovisioner.RoleControlPlane))
	assert.False(t, talosprovisioner.PatchAppliesToRoleForTest(
		talosconfigmanager.PatchScopeControlPlane, talosprovisioner.RoleWorker))
	assert.True(t, talosprovisioner.PatchAppliesToRoleForTest(
		talosconfigmanager.PatchScopeWorker, talosprovisioner.RoleWorker))
	assert.False(t, talosprovisioner.PatchAppliesToRoleForTest(
		talosconfigmanager.PatchScopeWorker, talosprovisioner.RoleControlPlane))
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
