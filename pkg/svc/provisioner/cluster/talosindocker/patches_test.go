package talosindockerprovisioner_test

import (
	"os"
	"path/filepath"
	"testing"

	talosindockerprovisioner "github.com/devantler-tech/ksail/v5/pkg/svc/provisioner/cluster/talosindocker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoadPatches_EmptyDirectory(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	// Create empty patch directory structure
	require.NoError(t, os.MkdirAll(filepath.Join(tempDir, "cluster"), 0o750))
	require.NoError(t, os.MkdirAll(filepath.Join(tempDir, "control-planes"), 0o750))
	require.NoError(t, os.MkdirAll(filepath.Join(tempDir, "workers"), 0o750))

	config := talosindockerprovisioner.NewTalosInDockerConfig().
		WithPatchesDir(tempDir)

	patches, err := talosindockerprovisioner.LoadPatches(config)
	require.NoError(t, err)
	assert.Empty(t, patches)
}

func TestLoadPatches_ClusterPatches(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	// Create patch directory structure
	clusterDir := filepath.Join(tempDir, "cluster")
	require.NoError(t, os.MkdirAll(clusterDir, 0o750))
	require.NoError(t, os.MkdirAll(filepath.Join(tempDir, "control-planes"), 0o750))
	require.NoError(t, os.MkdirAll(filepath.Join(tempDir, "workers"), 0o750))

	// Create a cluster-wide patch file
	patchContent := `machine:
  network:
    hostname: test-node
`
	patchPath := filepath.Join(clusterDir, "hostname.yaml")
	require.NoError(t, os.WriteFile(patchPath, []byte(patchContent), 0o600))

	config := talosindockerprovisioner.NewTalosInDockerConfig().
		WithPatchesDir(tempDir)

	patches, err := talosindockerprovisioner.LoadPatches(config)
	require.NoError(t, err)
	require.Len(t, patches, 1)

	assert.Equal(t, talosindockerprovisioner.PatchScopeCluster, patches[0].Scope)
	assert.Equal(t, patchPath, patches[0].Path)
	assert.Equal(t, []byte(patchContent), patches[0].Content)
}

func TestLoadPatches_ControlPlanePatches(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	// Create patch directory structure
	cpDir := filepath.Join(tempDir, "control-planes")
	require.NoError(t, os.MkdirAll(filepath.Join(tempDir, "cluster"), 0o750))
	require.NoError(t, os.MkdirAll(cpDir, 0o750))
	require.NoError(t, os.MkdirAll(filepath.Join(tempDir, "workers"), 0o750))

	// Create a control-plane patch file
	patchContent := `machine:
  certSANs:
    - 10.0.0.1
`
	patchPath := filepath.Join(cpDir, "cert-san.yaml")
	require.NoError(t, os.WriteFile(patchPath, []byte(patchContent), 0o600))

	config := talosindockerprovisioner.NewTalosInDockerConfig().
		WithPatchesDir(tempDir)

	patches, err := talosindockerprovisioner.LoadPatches(config)
	require.NoError(t, err)
	require.Len(t, patches, 1)

	assert.Equal(t, talosindockerprovisioner.PatchScopeControlPlane, patches[0].Scope)
	assert.Equal(t, patchPath, patches[0].Path)
	assert.Equal(t, []byte(patchContent), patches[0].Content)
}

func TestLoadPatches_WorkerPatches(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	// Create patch directory structure
	workerDir := filepath.Join(tempDir, "workers")
	require.NoError(t, os.MkdirAll(filepath.Join(tempDir, "cluster"), 0o750))
	require.NoError(t, os.MkdirAll(filepath.Join(tempDir, "control-planes"), 0o750))
	require.NoError(t, os.MkdirAll(workerDir, 0o750))

	// Create a worker patch file
	patchContent := `machine:
  kubelet:
    extraArgs:
      max-pods: "250"
`
	patchPath := filepath.Join(workerDir, "kubelet.yaml")
	require.NoError(t, os.WriteFile(patchPath, []byte(patchContent), 0o600))

	config := talosindockerprovisioner.NewTalosInDockerConfig().
		WithPatchesDir(tempDir)

	patches, err := talosindockerprovisioner.LoadPatches(config)
	require.NoError(t, err)
	require.Len(t, patches, 1)

	assert.Equal(t, talosindockerprovisioner.PatchScopeWorker, patches[0].Scope)
	assert.Equal(t, patchPath, patches[0].Path)
	assert.Equal(t, []byte(patchContent), patches[0].Content)
}

func TestLoadPatches_MultiplePatches(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	// Create patch directory structure
	clusterDir := filepath.Join(tempDir, "cluster")
	cpDir := filepath.Join(tempDir, "control-planes")
	workerDir := filepath.Join(tempDir, "workers")

	require.NoError(t, os.MkdirAll(clusterDir, 0o750))
	require.NoError(t, os.MkdirAll(cpDir, 0o750))
	require.NoError(t, os.MkdirAll(workerDir, 0o750))

	// Create patch files in each directory
	require.NoError(
		t,
		os.WriteFile(filepath.Join(clusterDir, "patch1.yaml"), []byte("cluster: patch1"), 0o600),
	)
	require.NoError(
		t,
		os.WriteFile(filepath.Join(clusterDir, "patch2.yaml"), []byte("cluster: patch2"), 0o600),
	)
	require.NoError(t, os.WriteFile(filepath.Join(cpDir, "cp.yaml"), []byte("cp: patch"), 0o600))
	require.NoError(
		t,
		os.WriteFile(filepath.Join(workerDir, "worker.yaml"), []byte("worker: patch"), 0o600),
	)

	config := talosindockerprovisioner.NewTalosInDockerConfig().
		WithPatchesDir(tempDir)

	patches, err := talosindockerprovisioner.LoadPatches(config)
	require.NoError(t, err)
	assert.Len(t, patches, 4)

	// Count patches by scope
	scopeCounts := make(map[talosindockerprovisioner.PatchScope]int)
	for _, p := range patches {
		scopeCounts[p.Scope]++
	}

	assert.Equal(t, 2, scopeCounts[talosindockerprovisioner.PatchScopeCluster])
	assert.Equal(t, 1, scopeCounts[talosindockerprovisioner.PatchScopeControlPlane])
	assert.Equal(t, 1, scopeCounts[talosindockerprovisioner.PatchScopeWorker])
}

func TestLoadPatches_IgnoresNonYAMLFiles(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	// Create patch directory structure
	clusterDir := filepath.Join(tempDir, "cluster")
	require.NoError(t, os.MkdirAll(clusterDir, 0o750))
	require.NoError(t, os.MkdirAll(filepath.Join(tempDir, "control-planes"), 0o750))
	require.NoError(t, os.MkdirAll(filepath.Join(tempDir, "workers"), 0o750))

	// Create various files - only .yaml and .yml should be loaded
	require.NoError(
		t,
		os.WriteFile(filepath.Join(clusterDir, "valid.yaml"), []byte("valid: yaml"), 0o600),
	)
	require.NoError(
		t,
		os.WriteFile(filepath.Join(clusterDir, "also-valid.yml"), []byte("also: valid"), 0o600),
	)
	require.NoError(t, os.WriteFile(filepath.Join(clusterDir, ".gitkeep"), []byte(""), 0o600))
	require.NoError(
		t,
		os.WriteFile(filepath.Join(clusterDir, "readme.md"), []byte("# README"), 0o600),
	)
	require.NoError(t, os.WriteFile(filepath.Join(clusterDir, "config.json"), []byte("{}"), 0o600))

	config := talosindockerprovisioner.NewTalosInDockerConfig().
		WithPatchesDir(tempDir)

	patches, err := talosindockerprovisioner.LoadPatches(config)
	require.NoError(t, err)
	assert.Len(t, patches, 2)
}

func TestLoadPatches_MissingDirectory(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	nonExistentDir := filepath.Join(tempDir, "does-not-exist")

	config := talosindockerprovisioner.NewTalosInDockerConfig().
		WithPatchesDir(nonExistentDir)

	// Should return empty patches without error when directory doesn't exist
	patches, err := talosindockerprovisioner.LoadPatches(config)
	require.NoError(t, err)
	assert.Empty(t, patches)
}

func TestLoadPatches_NilConfig(t *testing.T) {
	t.Parallel()

	patches, err := talosindockerprovisioner.LoadPatches(nil)
	require.Error(t, err)
	assert.Nil(t, patches)
}
