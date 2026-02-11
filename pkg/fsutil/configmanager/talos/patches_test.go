package talos_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/fsutil/configmanager/talos"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPatchScope_Constants(t *testing.T) {
	t.Parallel()

	// Verify that patch scope constants are defined as expected
	// These are used to categorize patches
	assert.NotEqual(t, talos.PatchScopeCluster, talos.PatchScopeControlPlane)
	assert.NotEqual(t, talos.PatchScopeCluster, talos.PatchScopeWorker)
	assert.NotEqual(t, talos.PatchScopeControlPlane, talos.PatchScopeWorker)
}

func TestPatch_Structure(t *testing.T) {
	t.Parallel()

	patch := talos.Patch{
		Path:    "/path/to/patch.yaml",
		Scope:   talos.PatchScopeCluster,
		Content: []byte("machine:\n  network:\n    hostname: test\n"),
	}

	assert.Equal(t, "/path/to/patch.yaml", patch.Path)
	assert.Equal(t, talos.PatchScopeCluster, patch.Scope)
	assert.NotEmpty(t, patch.Content)
}

func TestLoadPatches_NonExistentDirectory(t *testing.T) {
	t.Parallel()

	patches, err := talos.LoadPatches("nonexistent-directory")

	require.NoError(t, err)
	assert.Empty(t, patches, "Should return empty patches for nonexistent directory")
}

func TestLoadPatches_EmptyDirectory(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	patches, err := talos.LoadPatches(tmpDir)

	require.NoError(t, err)
	assert.Empty(t, patches)
}

func TestLoadPatches_ClusterPatches(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	clusterDir := filepath.Join(tmpDir, "cluster")

	require.NoError(t, os.MkdirAll(clusterDir, 0o750))

	patchContent := []byte("machine:\n  network:\n    hostname: cluster-node\n")
	patchFile := filepath.Join(clusterDir, "hostname.yaml")
	require.NoError(t, os.WriteFile(patchFile, patchContent, 0o600))

	patches, err := talos.LoadPatches(tmpDir)

	require.NoError(t, err)
	require.Len(t, patches, 1)
	assert.Equal(t, talos.PatchScopeCluster, patches[0].Scope)
	assert.Contains(t, patches[0].Path, "hostname.yaml")
	assert.NotEmpty(t, patches[0].Content)
}

func TestLoadPatches_ControlPlanePatches(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	cpDir := filepath.Join(tmpDir, "control-planes")

	require.NoError(t, os.MkdirAll(cpDir, 0o750))

	patchContent := []byte("machine:\n  controlPlane:\n    controllerManager: {}\n")
	require.NoError(t, os.WriteFile(filepath.Join(cpDir, "cp.yaml"), patchContent, 0o600))

	patches, err := talos.LoadPatches(tmpDir)

	require.NoError(t, err)
	require.Len(t, patches, 1)
	assert.Equal(t, talos.PatchScopeControlPlane, patches[0].Scope)
}

func TestLoadPatches_WorkerPatches(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	workerDir := filepath.Join(tmpDir, "workers")

	require.NoError(t, os.MkdirAll(workerDir, 0o750))

	patchContent := []byte("machine:\n  kubelet:\n    image: test\n")
	require.NoError(t, os.WriteFile(filepath.Join(workerDir, "kubelet.yaml"), patchContent, 0o600))

	patches, err := talos.LoadPatches(tmpDir)

	require.NoError(t, err)
	require.Len(t, patches, 1)
	assert.Equal(t, talos.PatchScopeWorker, patches[0].Scope)
}

func TestLoadPatches_MultipleScopes(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()

	// Create all three directories
	clusterDir := filepath.Join(tmpDir, "cluster")
	cpDir := filepath.Join(tmpDir, "control-planes")
	workerDir := filepath.Join(tmpDir, "workers")

	require.NoError(t, os.MkdirAll(clusterDir, 0o750))
	require.NoError(t, os.MkdirAll(cpDir, 0o750))
	require.NoError(t, os.MkdirAll(workerDir, 0o750))

	// Create patches in each directory
	require.NoError(t, os.WriteFile(
		filepath.Join(clusterDir, "cluster.yaml"),
		[]byte("machine:\n  network: {}\n"),
		0o600,
	))
	require.NoError(t, os.WriteFile(
		filepath.Join(cpDir, "cp.yaml"),
		[]byte("machine:\n  controlPlane: {}\n"),
		0o600,
	))
	require.NoError(t, os.WriteFile(
		filepath.Join(workerDir, "worker.yaml"),
		[]byte("machine:\n  kubelet: {}\n"),
		0o600,
	))

	patches, err := talos.LoadPatches(tmpDir)

	require.NoError(t, err)
	require.Len(t, patches, 3)

	// Count patches by scope
	scopeCounts := make(map[talos.PatchScope]int)
	for _, patch := range patches {
		scopeCounts[patch.Scope]++
	}

	assert.Equal(t, 1, scopeCounts[talos.PatchScopeCluster])
	assert.Equal(t, 1, scopeCounts[talos.PatchScopeControlPlane])
	assert.Equal(t, 1, scopeCounts[talos.PatchScopeWorker])
}

func TestLoadPatches_YMLExtension(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	clusterDir := filepath.Join(tmpDir, "cluster")

	require.NoError(t, os.MkdirAll(clusterDir, 0o750))

	// Use .yml extension instead of .yaml
	patchContent := []byte("machine:\n  network:\n    hostname: test\n")
	require.NoError(t, os.WriteFile(filepath.Join(clusterDir, "patch.yml"), patchContent, 0o600))

	patches, err := talos.LoadPatches(tmpDir)

	require.NoError(t, err)
	require.Len(t, patches, 1)
	assert.Contains(t, patches[0].Path, "patch.yml")
}

func TestLoadPatches_IgnoresNonYAMLFiles(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	clusterDir := filepath.Join(tmpDir, "cluster")

	require.NoError(t, os.MkdirAll(clusterDir, 0o750))

	// Create YAML file
	require.NoError(t, os.WriteFile(
		filepath.Join(clusterDir, "valid.yaml"),
		[]byte("machine:\n  network: {}\n"),
		0o600,
	))

	// Create non-YAML files that should be ignored
	readmeFile := filepath.Join(clusterDir, "README.md")
	require.NoError(t, os.WriteFile(readmeFile, []byte("# README"), 0o600))

	scriptFile := filepath.Join(clusterDir, "script.sh")
	require.NoError(t, os.WriteFile(scriptFile, []byte("#!/bin/bash"), 0o600))

	patches, err := talos.LoadPatches(tmpDir)

	require.NoError(t, err)
	assert.Len(t, patches, 1, "Should only load YAML files")
	assert.Contains(t, patches[0].Path, "valid.yaml")
}

func TestLoadPatches_MultipleYAMLFiles(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	clusterDir := filepath.Join(tmpDir, "cluster")

	require.NoError(t, os.MkdirAll(clusterDir, 0o750))

	// Create multiple YAML files
	filenames := []string{"patch1.yaml", "patch2.yaml", "patch3.yaml"}
	for _, filename := range filenames {
		fullPath := filepath.Join(clusterDir, filename)
		content := []byte("machine:\n  network: {}\n")
		require.NoError(t, os.WriteFile(fullPath, content, 0o600))
	}

	patches, err := talos.LoadPatches(tmpDir)

	require.NoError(t, err)
	assert.Len(t, patches, 3, "Should load all YAML files")
}

func TestConstants(t *testing.T) {
	t.Parallel()

	// Test that default constants are exported and have expected values
	assert.Equal(t, "talos", talos.DefaultPatchesDir)
	assert.Equal(t, "10.5.0.0/24", talos.DefaultNetworkCIDR)
	assert.Equal(t, "1.32.0", talos.DefaultKubernetesVersion)
	assert.NotEmpty(t, talos.DefaultClusterName)
	assert.Contains(t, talos.DefaultTalosImage, "talos")
}

func TestLoadPatches_ExpandsEnvVars(t *testing.T) {
	// Note: Cannot use t.Parallel() when using t.Setenv()
	tmpDir := t.TempDir()
	clusterDir := filepath.Join(tmpDir, "cluster")

	require.NoError(t, os.MkdirAll(clusterDir, 0o750))

	// Patch with environment variable placeholder
	patchContent := []byte("machine:\n  network:\n    hostname: ${TEST_HOSTNAME}\n")
	patchFile := filepath.Join(clusterDir, "hostname.yaml")
	require.NoError(t, os.WriteFile(patchFile, patchContent, 0o600))

	// Set environment variable
	t.Setenv("TEST_HOSTNAME", "expanded-host")

	patches, err := talos.LoadPatches(tmpDir)

	require.NoError(t, err)
	require.Len(t, patches, 1)

	// Verify the content was expanded
	assert.Contains(t, string(patches[0].Content), "hostname: expanded-host")
	assert.NotContains(t, string(patches[0].Content), "${TEST_HOSTNAME}")
}

//nolint:paralleltest // Uses t.Setenv
func TestLoadPatches_ExpandsEnvVarsWithDefault(
	t *testing.T,
) {
	tmpDir := t.TempDir()
	clusterDir := filepath.Join(tmpDir, "cluster")

	require.NoError(t, os.MkdirAll(clusterDir, 0o750))

	// Patch with default value syntax - UNDEFINED_VAR not set
	patchContent := []byte("machine:\n  network:\n    hostname: ${UNDEFINED_HOST:-default-host}\n")
	patchFile := filepath.Join(clusterDir, "hostname.yaml")
	require.NoError(t, os.WriteFile(patchFile, patchContent, 0o600))

	patches, err := talos.LoadPatches(tmpDir)

	require.NoError(t, err)
	require.Len(t, patches, 1)

	// Verify the default value was used
	assert.Contains(t, string(patches[0].Content), "hostname: default-host")
	assert.NotContains(t, string(patches[0].Content), "${UNDEFINED_HOST")
}
