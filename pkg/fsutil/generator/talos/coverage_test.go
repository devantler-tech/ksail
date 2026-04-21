package talosgenerator_test

import (
	"os"
	"path/filepath"
	"testing"

	talosgenerator "github.com/devantler-tech/ksail/v7/pkg/fsutil/generator/talos"
	yamlgenerator "github.com/devantler-tech/ksail/v7/pkg/fsutil/generator/yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGenerator_Generate_DisableCDI verifies that the DisableCDI flag generates
// the disable-cdi.yaml patch file.
func TestGenerator_Generate_DisableCDI(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	gen := talosgenerator.NewGenerator()

	config := &talosgenerator.Config{
		PatchesDir:  "talos",
		WorkerNodes: 1,
		DisableCDI:  true,
	}
	opts := yamlgenerator.Options{
		Output: tempDir,
		Force:  true,
	}

	result, err := gen.Generate(config, opts)
	require.NoError(t, err)
	assert.NotEmpty(t, result)

	patchPath := filepath.Join(tempDir, "talos", "cluster", "disable-cdi.yaml")
	//nolint:gosec // Test reads a file created in its own temp directory.
	content, err := os.ReadFile(patchPath)
	require.NoError(t, err, "disable-cdi.yaml should exist")
	assert.Contains(t, string(content), "enableCDI: false")
}

// TestGenerator_Generate_DisableCDISkipsExisting verifies that the disable-cdi
// patch is not overwritten when force is false.
func TestGenerator_Generate_DisableCDISkipsExisting(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	gen := talosgenerator.NewGenerator()

	config := &talosgenerator.Config{
		PatchesDir:  "talos",
		WorkerNodes: 1,
		DisableCDI:  true,
	}

	// First generate with force
	opts := yamlgenerator.Options{
		Output: tempDir,
		Force:  true,
	}

	_, err := gen.Generate(config, opts)
	require.NoError(t, err)

	// Overwrite with custom content
	patchPath := filepath.Join(tempDir, "talos", "cluster", "disable-cdi.yaml")
	err = os.WriteFile(patchPath, []byte("custom content"), 0o600)
	require.NoError(t, err)

	// Generate again without force - should not overwrite
	opts.Force = false
	_, err = gen.Generate(config, opts)
	require.NoError(t, err)

	//nolint:gosec // Test reads a file created in its own temp directory.
	content, err := os.ReadFile(patchPath)
	require.NoError(t, err)
	assert.Equal(t, "custom content", string(content))
}

// TestGenerator_Generate_DisableCDIOverwritesWithForce verifies that the disable-cdi
// patch is overwritten when force is true.
func TestGenerator_Generate_DisableCDIOverwritesWithForce(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	gen := talosgenerator.NewGenerator()

	config := &talosgenerator.Config{
		PatchesDir:  "talos",
		WorkerNodes: 1,
		DisableCDI:  true,
	}

	opts := yamlgenerator.Options{
		Output: tempDir,
		Force:  true,
	}

	_, err := gen.Generate(config, opts)
	require.NoError(t, err)

	// Overwrite
	patchPath := filepath.Join(tempDir, "talos", "cluster", "disable-cdi.yaml")
	err = os.WriteFile(patchPath, []byte("old content"), 0o600)
	require.NoError(t, err)

	// Generate again with force - should overwrite
	_, err = gen.Generate(config, opts)
	require.NoError(t, err)

	//nolint:gosec // Test reads a file created in its own temp directory.
	content, err := os.ReadFile(patchPath)
	require.NoError(t, err)
	assert.Contains(t, string(content), "enableCDI: false")
	assert.NotContains(t, string(content), "old content")
}

// TestGenerator_Generate_MirrorRegistriesEmptyHost verifies that mirror specs
// with empty host are skipped.
func TestGenerator_Generate_MirrorRegistriesEmptyHost(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	gen := talosgenerator.NewGenerator()

	config := &talosgenerator.Config{
		PatchesDir:       "talos",
		WorkerNodes:      1,
		MirrorRegistries: []string{"=http://mirror.local:5000"},
	}
	opts := yamlgenerator.Options{
		Output: tempDir,
		Force:  true,
	}

	_, err := gen.Generate(config, opts)
	require.NoError(t, err)

	// Since the host is empty, no mirror registries patch should be created
	// and the cluster dir should have .gitkeep instead
	patchPath := filepath.Join(tempDir, "talos", "cluster", "mirror-registries.yaml")
	_, statErr := os.Stat(patchPath)
	assert.True(
		t,
		os.IsNotExist(statErr),
		"mirror-registries.yaml should not be created for empty host",
	)
}

// TestGenerator_Generate_DisableCDIDirectoryHasPatches verifies that the cluster
// directory does NOT get .gitkeep when DisableCDI is true.
func TestGenerator_Generate_DisableCDIDirectoryHasPatches(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	gen := talosgenerator.NewGenerator()

	config := &talosgenerator.Config{
		PatchesDir:  "talos",
		WorkerNodes: 1,
		DisableCDI:  true,
	}
	opts := yamlgenerator.Options{
		Output: tempDir,
		Force:  true,
	}

	_, err := gen.Generate(config, opts)
	require.NoError(t, err)

	// cluster/ should NOT have .gitkeep since it has patches
	gitkeepPath := filepath.Join(tempDir, "talos", "cluster", ".gitkeep")
	_, statErr := os.Stat(gitkeepPath)
	assert.True(t, os.IsNotExist(statErr), ".gitkeep should not exist when patches are present")

	// but control-planes/ and workers/ should have .gitkeep
	cpGitkeep := filepath.Join(tempDir, "talos", "control-planes", ".gitkeep")
	_, statErr = os.Stat(cpGitkeep)
	assert.NoError(t, statErr, "control-planes/.gitkeep should exist")
}

// TestGenerator_Generate_ImageVerificationPatch verifies that the image verification
// config document is generated.
func TestGenerator_Generate_ImageVerificationPatch(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	gen := talosgenerator.NewGenerator()

	config := &talosgenerator.Config{
		PatchesDir:              "talos",
		WorkerNodes:             1,
		EnableImageVerification: true,
	}
	opts := yamlgenerator.Options{
		Output: tempDir,
		Force:  true,
	}

	_, err := gen.Generate(config, opts)
	require.NoError(t, err)

	patchPath := filepath.Join(tempDir, "talos", "cluster", "image-verification.yaml")
	//nolint:gosec // Test reads a file created in its own temp directory.
	content, err := os.ReadFile(patchPath)
	require.NoError(t, err, "image-verification.yaml should exist")
	assert.Contains(t, string(content), "ImageVerificationConfig")
}

// TestGenerator_Generate_ClusterNamePatch verifies that the cluster name patch is generated.
func TestGenerator_Generate_ClusterNamePatch(t *testing.T) {
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
		Force:  true,
	}

	_, err := gen.Generate(config, opts)
	require.NoError(t, err)

	patchPath := filepath.Join(tempDir, "talos", "cluster", "cluster-name.yaml")
	//nolint:gosec // Test reads a file created in its own temp directory.
	content, err := os.ReadFile(patchPath)
	require.NoError(t, err, "cluster-name.yaml should exist")
	assert.Contains(t, string(content), "my-custom-cluster")
}

// TestGenerator_Generate_AllConditionalPatches verifies that all conditional
// patches are generated when all features are enabled.
func TestGenerator_Generate_AllConditionalPatches(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	gen := talosgenerator.NewGenerator()

	config := &talosgenerator.Config{
		PatchesDir:                "talos",
		WorkerNodes:               0,
		DisableDefaultCNI:         true,
		EnableKubeletCertRotation: true,
		ClusterName:               "all-patches",
		EnableImageVerification:   true,
		DisableCDI:                true,
		MirrorRegistries:          []string{"docker.io=http://mirror.local:5000"},
	}
	opts := yamlgenerator.Options{
		Output: tempDir,
		Force:  true,
	}

	_, err := gen.Generate(config, opts)
	require.NoError(t, err)

	expectedFiles := []string{
		"mirror-registries.yaml",
		"allow-scheduling-on-control-planes.yaml",
		"disable-default-cni.yaml",
		"kubelet-cert-rotation.yaml",
		"kubelet-csr-approver.yaml",
		"cluster-name.yaml",
		"image-verification.yaml",
		"disable-cdi.yaml",
	}

	for _, filename := range expectedFiles {
		patchPath := filepath.Join(tempDir, "talos", "cluster", filename)
		_, statErr := os.Stat(patchPath)
		assert.NoError(t, statErr, "expected file to exist: %s", filename)
	}
}

// TestGenerator_Generate_ConfiguredOutput verifies the configured output directory is used.
func TestGenerator_Generate_ConfiguredOutput(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	gen := talosgenerator.NewGenerator()

	config := &talosgenerator.Config{
		PatchesDir:  "talos",
		WorkerNodes: 1,
	}
	opts := yamlgenerator.Options{
		Output: tempDir,
	}

	result, err := gen.Generate(config, opts)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(tempDir, "talos"), result)
}
