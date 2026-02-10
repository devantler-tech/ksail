package talosgenerator_test

import (
	"os"
	"path/filepath"
	"testing"

	talosgenerator "github.com/devantler-tech/ksail/v5/pkg/fsutil/generator/talos"
	yamlgenerator "github.com/devantler-tech/ksail/v5/pkg/fsutil/generator/yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewGenerator(t *testing.T) {
	t.Parallel()

	gen := talosgenerator.NewGenerator()
	require.NotNil(t, gen)
}

func TestGenerator_Generate_CreatesDirectoryStructure(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	gen := talosgenerator.NewGenerator()

	// With workers > 0 and no other patches, all dirs should have .gitkeep
	config := &talosgenerator.Config{
		PatchesDir:  "talos",
		WorkerNodes: 1, // Prevents allow-scheduling patch from being generated
	}
	opts := yamlgenerator.Options{
		Output: tempDir,
	}

	result, err := gen.Generate(config, opts)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(tempDir, "talos"), result)

	// Verify directory structure - all should have .gitkeep since no patches generated
	expectedPaths := []string{
		filepath.Join(tempDir, "talos", "cluster", ".gitkeep"),
		filepath.Join(tempDir, "talos", "control-planes", ".gitkeep"),
		filepath.Join(tempDir, "talos", "workers", ".gitkeep"),
	}

	for _, path := range expectedPaths {
		info, err := os.Stat(path)
		require.NoError(t, err, "expected path to exist: %s", path)
		assert.False(t, info.IsDir(), "expected file, got directory: %s", path)
	}
}

func TestGenerator_Generate_NilConfig(t *testing.T) {
	t.Parallel()

	gen := talosgenerator.NewGenerator()
	opts := yamlgenerator.Options{
		Output: t.TempDir(),
	}

	result, err := gen.Generate(nil, opts)
	require.Error(t, err)
	assert.Empty(t, result)
	assert.Contains(t, err.Error(), "config is required")
}

func TestGenerator_Generate_DefaultPatchesDir(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	gen := talosgenerator.NewGenerator()

	config := &talosgenerator.Config{
		PatchesDir:  "", // Empty should default to "talos"
		WorkerNodes: 1,  // Prevents allow-scheduling patch
	}
	opts := yamlgenerator.Options{
		Output: tempDir,
	}

	result, err := gen.Generate(config, opts)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(tempDir, "talos"), result)

	// Verify the default directory was created with .gitkeep
	_, err = os.Stat(filepath.Join(tempDir, "talos", "cluster", ".gitkeep"))
	require.NoError(t, err)
}

func TestGenerator_Generate_CustomPatchesDir(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	gen := talosgenerator.NewGenerator()

	config := &talosgenerator.Config{
		PatchesDir:  "custom-patches",
		WorkerNodes: 1, // Prevents allow-scheduling patch
	}
	opts := yamlgenerator.Options{
		Output: tempDir,
	}

	result, err := gen.Generate(config, opts)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(tempDir, "custom-patches"), result)

	// Verify the custom directory was created with .gitkeep
	_, err = os.Stat(filepath.Join(tempDir, "custom-patches", "cluster", ".gitkeep"))
	require.NoError(t, err)
}

//nolint:paralleltest // t.Chdir cannot be used with t.Parallel
func TestGenerator_Generate_DefaultOutputDir(t *testing.T) {
	// Create a temporary directory and change to it
	tempDir := t.TempDir()
	t.Chdir(tempDir)

	gen := talosgenerator.NewGenerator()

	config := &talosgenerator.Config{
		PatchesDir:  "talos",
		WorkerNodes: 1, // Prevents allow-scheduling patch
	}
	opts := yamlgenerator.Options{
		Output: "", // Empty should default to "."
	}

	result, err := gen.Generate(config, opts)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(".", "talos"), result)

	// Verify directory was created in current directory with .gitkeep
	_, err = os.Stat(filepath.Join(".", "talos", "cluster", ".gitkeep"))
	require.NoError(t, err)
}

func TestGenerator_Generate_SkipsExistingWithoutForce(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	gen := talosgenerator.NewGenerator()

	// Create an existing .gitkeep with custom content
	clusterDir := filepath.Join(tempDir, "talos", "cluster")
	err := os.MkdirAll(clusterDir, 0o750)
	require.NoError(t, err)

	gitkeepPath := filepath.Join(clusterDir, ".gitkeep")
	err = os.WriteFile(gitkeepPath, []byte("existing content"), 0o600)
	require.NoError(t, err)

	config := &talosgenerator.Config{
		PatchesDir:  "talos",
		WorkerNodes: 1, // Prevents allow-scheduling patch, so .gitkeep should be preserved
	}
	opts := yamlgenerator.Options{
		Output: tempDir,
		Force:  false,
	}

	result, err := gen.Generate(config, opts)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(tempDir, "talos"), result)

	// Verify existing file content was preserved
	content, err := os.ReadFile(gitkeepPath) //nolint:gosec // Test file path is safe
	require.NoError(t, err)
	assert.Equal(t, "existing content", string(content))
}

func TestGenerator_Generate_OverwritesWithForce(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	gen := talosgenerator.NewGenerator()

	// Create an existing .gitkeep with custom content
	clusterDir := filepath.Join(tempDir, "talos", "cluster")
	err := os.MkdirAll(clusterDir, 0o750)
	require.NoError(t, err)

	gitkeepPath := filepath.Join(clusterDir, ".gitkeep")
	err = os.WriteFile(gitkeepPath, []byte("existing content"), 0o600)
	require.NoError(t, err)

	config := &talosgenerator.Config{
		PatchesDir:  "talos",
		WorkerNodes: 1, // Prevents allow-scheduling patch, so .gitkeep should be written
	}
	opts := yamlgenerator.Options{
		Output: tempDir,
		Force:  true,
	}

	result, err := gen.Generate(config, opts)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(tempDir, "talos"), result)

	// Verify file was overwritten (now empty)
	content, err := os.ReadFile(gitkeepPath) //nolint:gosec // Test file path is safe
	require.NoError(t, err)
	assert.Empty(t, string(content))
}

func TestGenerator_Generate_DisableDefaultCNI(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	gen := talosgenerator.NewGenerator()

	config := &talosgenerator.Config{
		PatchesDir:        "talos",
		DisableDefaultCNI: true,
		WorkerNodes:       1, // Prevents allow-scheduling patch
	}
	opts := yamlgenerator.Options{
		Output: tempDir,
	}

	result, err := gen.Generate(config, opts)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(tempDir, "talos"), result)

	// Verify disable-default-cni.yaml was created
	patchPath := filepath.Join(tempDir, "talos", "cluster", "disable-default-cni.yaml")
	content, err := os.ReadFile(patchPath) //nolint:gosec // Test file path is safe
	require.NoError(t, err)
	assert.Contains(t, string(content), "cluster:")
	assert.Contains(t, string(content), "network:")
	assert.Contains(t, string(content), "cni:")
	assert.Contains(t, string(content), "name: none")

	// Verify .gitkeep was NOT created in cluster/ since we have a patch there
	gitkeepPath := filepath.Join(tempDir, "talos", "cluster", ".gitkeep")
	_, err = os.Stat(gitkeepPath)
	assert.True(t, os.IsNotExist(err), "expected .gitkeep to not exist when patches are generated")

	// Verify .gitkeep WAS created in other directories
	_, err = os.Stat(filepath.Join(tempDir, "talos", "control-planes", ".gitkeep"))
	require.NoError(t, err, "expected .gitkeep in control-planes/")
	_, err = os.Stat(filepath.Join(tempDir, "talos", "workers", ".gitkeep"))
	require.NoError(t, err, "expected .gitkeep in workers/")
}

func TestGenerator_Generate_NoDisableCNIPatchWhenFalse(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	gen := talosgenerator.NewGenerator()

	config := &talosgenerator.Config{
		PatchesDir:        "talos",
		DisableDefaultCNI: false,
		WorkerNodes:       1, // Prevents allow-scheduling patch
	}
	opts := yamlgenerator.Options{
		Output: tempDir,
	}

	_, err := gen.Generate(config, opts)
	require.NoError(t, err)

	// Verify disable-default-cni.yaml was NOT created
	patchPath := filepath.Join(tempDir, "talos", "cluster", "disable-default-cni.yaml")
	_, err = os.Stat(patchPath)
	assert.True(t, os.IsNotExist(err), "expected disable-default-cni.yaml to not exist")

	// Verify .gitkeep WAS created since no patches in cluster/
	gitkeepPath := filepath.Join(tempDir, "talos", "cluster", ".gitkeep")
	_, err = os.Stat(gitkeepPath)
	require.NoError(t, err, "expected .gitkeep in cluster/ when no patches generated")
}

func TestGenerator_Generate_AllowSchedulingOnControlPlanes(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	gen := talosgenerator.NewGenerator()

	config := &talosgenerator.Config{
		PatchesDir:  "talos",
		WorkerNodes: 0, // Zero workers triggers allow-scheduling patch
	}
	opts := yamlgenerator.Options{
		Output: tempDir,
	}

	result, err := gen.Generate(config, opts)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(tempDir, "talos"), result)

	// Verify allow-scheduling-on-control-planes.yaml was created
	clusterDir := filepath.Join(tempDir, "talos", "cluster")
	patchPath := filepath.Join(clusterDir, "allow-scheduling-on-control-planes.yaml")
	content, err := os.ReadFile(patchPath) //nolint:gosec // Test file path is safe
	require.NoError(t, err)
	assert.Contains(t, string(content), "cluster:")
	assert.Contains(t, string(content), "allowSchedulingOnControlPlanes: true")

	// Verify .gitkeep was NOT created in cluster/ since we have a patch there
	gitkeepPath := filepath.Join(clusterDir, ".gitkeep")
	_, err = os.Stat(gitkeepPath)
	assert.True(t, os.IsNotExist(err), "expected .gitkeep to not exist when patches are generated")
}

func TestGenerator_Generate_MirrorRegistries(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	gen := talosgenerator.NewGenerator()

	config := &talosgenerator.Config{
		PatchesDir:  "talos",
		WorkerNodes: 1,
		MirrorRegistries: []string{
			"docker.io=https://registry-1.docker.io",
			"gcr.io=https://gcr.io",
		},
	}
	opts := yamlgenerator.Options{
		Output: tempDir,
	}

	result, err := gen.Generate(config, opts)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(tempDir, "talos"), result)

	// Verify mirror-registries.yaml was created
	patchPath := filepath.Join(tempDir, "talos", "cluster", "mirror-registries.yaml")
	content, err := os.ReadFile(patchPath) //nolint:gosec // Test file path is safe
	require.NoError(t, err)
	assert.Contains(t, string(content), "machine:")
	assert.Contains(t, string(content), "registries:")
	assert.Contains(t, string(content), "mirrors:")
	assert.Contains(t, string(content), "docker.io:")
	assert.Contains(t, string(content), "gcr.io:")
	assert.Contains(t, string(content), "endpoints:")
	assert.Contains(t, string(content), "http://")

	// Verify .gitkeep was NOT created in cluster/ since we have a patch there
	gitkeepPath := filepath.Join(tempDir, "talos", "cluster", ".gitkeep")
	_, err = os.Stat(gitkeepPath)
	assert.True(t, os.IsNotExist(err), "expected .gitkeep to not exist when patches are generated")
}

func TestGenerator_Generate_EmptyMirrorRegistries(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	gen := talosgenerator.NewGenerator()

	config := &talosgenerator.Config{
		PatchesDir:       "talos",
		WorkerNodes:      1,
		MirrorRegistries: []string{}, // Empty array should not create patch
	}
	opts := yamlgenerator.Options{
		Output: tempDir,
	}

	_, err := gen.Generate(config, opts)
	require.NoError(t, err)

	// Verify mirror-registries.yaml was NOT created
	patchPath := filepath.Join(tempDir, "talos", "cluster", "mirror-registries.yaml")
	_, err = os.Stat(patchPath)
	assert.True(t, os.IsNotExist(err), "expected mirror-registries.yaml to not exist")
}

func TestGenerator_Generate_KubeletCertRotation(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	gen := talosgenerator.NewGenerator()

	config := &talosgenerator.Config{
		PatchesDir:                "talos",
		WorkerNodes:               1,
		EnableKubeletCertRotation: true,
	}
	opts := yamlgenerator.Options{
		Output: tempDir,
	}

	result, err := gen.Generate(config, opts)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(tempDir, "talos"), result)

	// Verify kubelet-cert-rotation.yaml was created
	certRotationPath := filepath.Join(tempDir, "talos", "cluster", "kubelet-cert-rotation.yaml")
	content, err := os.ReadFile(certRotationPath) //nolint:gosec // Test file path is safe
	require.NoError(t, err)
	assert.Contains(t, string(content), "machine:")
	assert.Contains(t, string(content), "kubelet:")
	assert.Contains(t, string(content), "extraArgs:")
	assert.Contains(t, string(content), "rotate-server-certificates")
	assert.Contains(t, string(content), `"true"`)

	// Verify kubelet-csr-approver.yaml was also created
	csrApproverPath := filepath.Join(tempDir, "talos", "cluster", "kubelet-csr-approver.yaml")
	csrContent, err := os.ReadFile(csrApproverPath) //nolint:gosec // Test file path is safe
	require.NoError(t, err)
	assert.Contains(t, string(csrContent), "cluster:")
	assert.Contains(t, string(csrContent), "extraManifests:")
	assert.Contains(t, string(csrContent), talosgenerator.KubeletServingCertApproverManifestURL)
}

func TestGenerator_Generate_NoKubeletCertRotationPatchWhenFalse(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	gen := talosgenerator.NewGenerator()

	config := &talosgenerator.Config{
		PatchesDir:                "talos",
		WorkerNodes:               1,
		EnableKubeletCertRotation: false,
	}
	opts := yamlgenerator.Options{
		Output: tempDir,
	}

	_, err := gen.Generate(config, opts)
	require.NoError(t, err)

	// Verify kubelet-cert-rotation.yaml was NOT created
	certRotationPath := filepath.Join(tempDir, "talos", "cluster", "kubelet-cert-rotation.yaml")
	_, err = os.Stat(certRotationPath)
	assert.True(t, os.IsNotExist(err), "expected kubelet-cert-rotation.yaml to not exist")

	// Verify kubelet-csr-approver.yaml was also NOT created
	csrApproverPath := filepath.Join(tempDir, "talos", "cluster", "kubelet-csr-approver.yaml")
	_, err = os.Stat(csrApproverPath)
	assert.True(t, os.IsNotExist(err), "expected kubelet-csr-approver.yaml to not exist")
}

func TestGenerator_Generate_ClusterName(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	gen := talosgenerator.NewGenerator()

	config := &talosgenerator.Config{
		PatchesDir:  "talos",
		WorkerNodes: 1,
		ClusterName: "my-custom-cluster",
	}
	opts := yamlgenerator.Options{
		Output: tempDir,
	}

	result, err := gen.Generate(config, opts)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(tempDir, "talos"), result)

	// Verify cluster-name.yaml was created
	patchPath := filepath.Join(tempDir, "talos", "cluster", "cluster-name.yaml")
	content, err := os.ReadFile(patchPath) //nolint:gosec // Test file path is safe
	require.NoError(t, err)
	assert.Contains(t, string(content), "cluster:")
	assert.Contains(t, string(content), "clusterName: my-custom-cluster")
}

func TestGenerator_Generate_NoClusterNamePatchWhenEmpty(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	gen := talosgenerator.NewGenerator()

	config := &talosgenerator.Config{
		PatchesDir:  "talos",
		WorkerNodes: 1,
		ClusterName: "", // Empty should not create patch
	}
	opts := yamlgenerator.Options{
		Output: tempDir,
	}

	_, err := gen.Generate(config, opts)
	require.NoError(t, err)

	// Verify cluster-name.yaml was NOT created
	patchPath := filepath.Join(tempDir, "talos", "cluster", "cluster-name.yaml")
	_, err = os.Stat(patchPath)
	assert.True(t, os.IsNotExist(err), "expected cluster-name.yaml to not exist")
}

func TestGenerator_Generate_AllPatchesCombined(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	gen := talosgenerator.NewGenerator()

	config := &talosgenerator.Config{
		PatchesDir:                "talos",
		WorkerNodes:               0, // Triggers allow-scheduling patch
		MirrorRegistries:          []string{"docker.io=https://registry-1.docker.io"},
		DisableDefaultCNI:         true,
		EnableKubeletCertRotation: true,
		ClusterName:               "test-cluster",
	}
	opts := yamlgenerator.Options{
		Output: tempDir,
	}

	result, err := gen.Generate(config, opts)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(tempDir, "talos"), result)

	// Verify all patches were created
	clusterDir := filepath.Join(tempDir, "talos", "cluster")

	// Check allow-scheduling patch
	_, err = os.Stat(filepath.Join(clusterDir, "allow-scheduling-on-control-planes.yaml"))
	require.NoError(t, err, "expected allow-scheduling-on-control-planes.yaml")

	// Check mirror registries patch
	_, err = os.Stat(filepath.Join(clusterDir, "mirror-registries.yaml"))
	require.NoError(t, err, "expected mirror-registries.yaml")

	// Check disable CNI patch
	_, err = os.Stat(filepath.Join(clusterDir, "disable-default-cni.yaml"))
	require.NoError(t, err, "expected disable-default-cni.yaml")

	// Check kubelet cert rotation patch
	_, err = os.Stat(filepath.Join(clusterDir, "kubelet-cert-rotation.yaml"))
	require.NoError(t, err, "expected kubelet-cert-rotation.yaml")

	// Check kubelet CSR approver patch
	_, err = os.Stat(filepath.Join(clusterDir, "kubelet-csr-approver.yaml"))
	require.NoError(t, err, "expected kubelet-csr-approver.yaml")

	// Check cluster name patch
	_, err = os.Stat(filepath.Join(clusterDir, "cluster-name.yaml"))
	require.NoError(t, err, "expected cluster-name.yaml")

	// Verify .gitkeep was NOT created in cluster/ since we have patches there
	gitkeepPath := filepath.Join(clusterDir, ".gitkeep")
	_, err = os.Stat(gitkeepPath)
	assert.True(t, os.IsNotExist(err), "expected .gitkeep to not exist when patches are generated")

	// Verify .gitkeep WAS created in other directories
	_, err = os.Stat(filepath.Join(tempDir, "talos", "control-planes", ".gitkeep"))
	require.NoError(t, err, "expected .gitkeep in control-planes/")
	_, err = os.Stat(filepath.Join(tempDir, "talos", "workers", ".gitkeep"))
	require.NoError(t, err, "expected .gitkeep in workers/")
}

func TestGenerator_Generate_SkipsExistingPatchesWithoutForce(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	gen := talosgenerator.NewGenerator()

	// Create an existing patch with custom content
	clusterDir := filepath.Join(tempDir, "talos", "cluster")
	err := os.MkdirAll(clusterDir, 0o750)
	require.NoError(t, err)

	patchPath := filepath.Join(clusterDir, "disable-default-cni.yaml")
	err = os.WriteFile(patchPath, []byte("existing content"), 0o600)
	require.NoError(t, err)

	config := &talosgenerator.Config{
		PatchesDir:        "talos",
		WorkerNodes:       1,
		DisableDefaultCNI: true,
	}
	opts := yamlgenerator.Options{
		Output: tempDir,
		Force:  false,
	}

	result, err := gen.Generate(config, opts)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(tempDir, "talos"), result)

	// Verify existing file content was preserved
	content, err := os.ReadFile(patchPath) //nolint:gosec // Test file path is safe
	require.NoError(t, err)
	assert.Equal(t, "existing content", string(content))
}

func TestGenerator_Generate_OverwritesExistingPatchesWithForce(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	gen := talosgenerator.NewGenerator()

	// Create an existing patch with custom content
	clusterDir := filepath.Join(tempDir, "talos", "cluster")
	err := os.MkdirAll(clusterDir, 0o750)
	require.NoError(t, err)

	patchPath := filepath.Join(clusterDir, "disable-default-cni.yaml")
	err = os.WriteFile(patchPath, []byte("existing content"), 0o600)
	require.NoError(t, err)

	config := &talosgenerator.Config{
		PatchesDir:        "talos",
		WorkerNodes:       1,
		DisableDefaultCNI: true,
	}
	opts := yamlgenerator.Options{
		Output: tempDir,
		Force:  true,
	}

	result, err := gen.Generate(config, opts)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(tempDir, "talos"), result)

	// Verify file was overwritten with new content
	content, err := os.ReadFile(patchPath) //nolint:gosec // Test file path is safe
	require.NoError(t, err)
	assert.Contains(t, string(content), "cluster:")
	assert.Contains(t, string(content), "cni:")
	assert.Contains(t, string(content), "name: none")
}
