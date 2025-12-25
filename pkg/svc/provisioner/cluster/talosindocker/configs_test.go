package talosindockerprovisioner_test

import (
	"os"
	"path/filepath"
	"testing"

	talosindockerprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/talosindocker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupTestPatchDirs creates a temp directory structure for Talos patches.
func setupTestPatchDirs(t *testing.T) string {
	t.Helper()
	tempDir := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(tempDir, "talos", "cluster"), 0o750))
	require.NoError(t, os.MkdirAll(filepath.Join(tempDir, "talos", "control-planes"), 0o750))
	require.NoError(t, os.MkdirAll(filepath.Join(tempDir, "talos", "workers"), 0o750))

	return tempDir
}

// writePatch writes a patch file to the specified directory.
func writePatch(t *testing.T, dir, filename, content string) {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, filename), []byte(content), 0o600))
}

func TestTalosConfigs_LoadConfigs_NoPatches(t *testing.T) {
	t.Parallel()
	tempDir := setupTestPatchDirs(t)
	writePatch(t, filepath.Join(tempDir, "talos", "cluster"), ".gitkeep", "")
	writePatch(t, filepath.Join(tempDir, "talos", "control-planes"), ".gitkeep", "")
	writePatch(t, filepath.Join(tempDir, "talos", "workers"), ".gitkeep", "")

	config := talosindockerprovisioner.NewTalosInDockerConfig().
		WithClusterName("test-cluster").
		WithPatchesDir(filepath.Join(tempDir, "talos"))

	configs, err := config.LoadConfigs()
	require.NoError(t, err)
	require.NotNil(t, configs)
	assert.NotNil(t, configs.ControlPlane())
	assert.NotNil(t, configs.Worker())
	assert.NotNil(t, configs.Bundle())
	assert.Equal(t, "test-cluster", configs.ControlPlane().Cluster().Name())
	assert.Equal(t, "test-cluster", configs.Worker().Cluster().Name())
}

func TestTalosConfigs_LoadConfigs_ClusterPatch(t *testing.T) {
	t.Parallel()
	tempDir := setupTestPatchDirs(t)
	writePatch(t, filepath.Join(tempDir, "talos", "cluster"), "disable-cni.yaml", `cluster:
  network:
    cni:
      name: none
`)

	config := talosindockerprovisioner.NewTalosInDockerConfig().
		WithClusterName("test-cluster").
		WithPatchesDir(filepath.Join(tempDir, "talos"))

	configs, err := config.LoadConfigs()
	require.NoError(t, err)
	require.NotNil(t, configs)
	assert.Equal(t, "none", configs.ControlPlane().Cluster().Network().CNI().Name())
	assert.Equal(t, "none", configs.Worker().Cluster().Network().CNI().Name())
}

func TestTalosConfigs_LoadConfigs_ControlPlanePatch(t *testing.T) {
	t.Parallel()
	tempDir := setupTestPatchDirs(t)
	writePatch(t, filepath.Join(tempDir, "talos", "control-planes"), "kubelet.yaml", `machine:
  kubelet:
    extraArgs:
      cloud-provider: external
`)

	config := talosindockerprovisioner.NewTalosInDockerConfig().
		WithClusterName("test-cluster").
		WithPatchesDir(filepath.Join(tempDir, "talos"))

	configs, err := config.LoadConfigs()
	require.NoError(t, err)
	require.NotNil(t, configs)
	assert.Equal(
		t,
		"external",
		configs.ControlPlane().Machine().Kubelet().ExtraArgs()["cloud-provider"],
	)
	assert.Empty(t, configs.Worker().Machine().Kubelet().ExtraArgs()["cloud-provider"])
}

func TestTalosConfigs_LoadConfigs_WorkerPatch(t *testing.T) {
	t.Parallel()
	tempDir := setupTestPatchDirs(t)
	writePatch(t, filepath.Join(tempDir, "talos", "workers"), "labels.yaml", `machine:
  kubelet:
    extraArgs:
      node-labels: "role=worker"
`)

	config := talosindockerprovisioner.NewTalosInDockerConfig().
		WithClusterName("test-cluster").
		WithPatchesDir(filepath.Join(tempDir, "talos"))

	configs, err := config.LoadConfigs()
	require.NoError(t, err)
	require.NotNil(t, configs)
	assert.Equal(t, "role=worker", configs.Worker().Machine().Kubelet().ExtraArgs()["node-labels"])
	assert.Empty(t, configs.ControlPlane().Machine().Kubelet().ExtraArgs()["node-labels"])
}

func TestTalosConfigs_LoadConfigs_ProgrammaticAccess(t *testing.T) {
	t.Parallel()
	tempDir := setupTestPatchDirs(t)

	config := talosindockerprovisioner.NewTalosInDockerConfig().
		WithClusterName("my-cluster").
		WithKubernetesVersion("1.32.0").
		WithPatchesDir(filepath.Join(tempDir, "talos"))

	configs, err := config.LoadConfigs()
	require.NoError(t, err)
	require.NotNil(t, configs)

	cpConfig := configs.ControlPlane()
	assert.Equal(t, "my-cluster", cpConfig.Cluster().Name())
	assert.NotEmpty(t, cpConfig.Cluster().ID())
	assert.NotEmpty(t, cpConfig.Cluster().Secret())
	assert.Equal(t, "flannel", cpConfig.Cluster().Network().CNI().Name())
	assert.Equal(t, "controlplane", cpConfig.Machine().Type().String())
	assert.Equal(t, "worker", configs.Worker().Machine().Type().String())
}

func TestTalosConfigs_LoadConfigsWithPatches(t *testing.T) {
	t.Parallel()
	tempDir := setupTestPatchDirs(t)

	config := talosindockerprovisioner.NewTalosInDockerConfig().
		WithClusterName("test-cluster").
		WithPatchesDir(filepath.Join(tempDir, "talos"))

	inMemoryPatch := talosindockerprovisioner.TalosPatch{
		Path:  "in-memory:disable-cni",
		Scope: talosindockerprovisioner.PatchScopeCluster,
		Content: []byte(`cluster:
  network:
    cni:
      name: none
`),
	}

	configs, err := config.LoadConfigsWithPatches(
		[]talosindockerprovisioner.TalosPatch{inMemoryPatch},
	)
	require.NoError(t, err)
	require.NotNil(t, configs)
	assert.Equal(t, "none", configs.ControlPlane().Cluster().Network().CNI().Name())
	assert.Equal(t, "none", configs.Worker().Cluster().Network().CNI().Name())
}
