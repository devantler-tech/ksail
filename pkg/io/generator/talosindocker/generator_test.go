package talosgenerator_test

import (
	"os"
	"path/filepath"
	"testing"

	talosgenerator "github.com/devantler-tech/ksail/v5/pkg/io/generator/talosindocker"
	yamlgenerator "github.com/devantler-tech/ksail/v5/pkg/io/generator/yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewTalosInDockerGenerator(t *testing.T) {
	t.Parallel()

	gen := talosgenerator.NewTalosInDockerGenerator()
	require.NotNil(t, gen)
}

func TestTalosInDockerGenerator_Generate_CreatesDirectoryStructure(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	gen := talosgenerator.NewTalosInDockerGenerator()

	// With workers > 0 and no other patches, all dirs should have .gitkeep
	config := &talosgenerator.TalosInDockerConfig{
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

func TestTalosInDockerGenerator_Generate_NilConfig(t *testing.T) {
	t.Parallel()

	gen := talosgenerator.NewTalosInDockerGenerator()
	opts := yamlgenerator.Options{
		Output: t.TempDir(),
	}

	result, err := gen.Generate(nil, opts)
	require.Error(t, err)
	assert.Empty(t, result)
	assert.Contains(t, err.Error(), "config is required")
}

func TestTalosInDockerGenerator_Generate_DefaultPatchesDir(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	gen := talosgenerator.NewTalosInDockerGenerator()

	config := &talosgenerator.TalosInDockerConfig{
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

func TestTalosInDockerGenerator_Generate_CustomPatchesDir(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	gen := talosgenerator.NewTalosInDockerGenerator()

	config := &talosgenerator.TalosInDockerConfig{
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
func TestTalosInDockerGenerator_Generate_DefaultOutputDir(t *testing.T) {
	// Create a temporary directory and change to it
	tempDir := t.TempDir()
	t.Chdir(tempDir)

	gen := talosgenerator.NewTalosInDockerGenerator()

	config := &talosgenerator.TalosInDockerConfig{
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

func TestTalosInDockerGenerator_Generate_SkipsExistingWithoutForce(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	gen := talosgenerator.NewTalosInDockerGenerator()

	// Create an existing .gitkeep with custom content
	clusterDir := filepath.Join(tempDir, "talos", "cluster")
	err := os.MkdirAll(clusterDir, 0o750)
	require.NoError(t, err)

	gitkeepPath := filepath.Join(clusterDir, ".gitkeep")
	err = os.WriteFile(gitkeepPath, []byte("existing content"), 0o600)
	require.NoError(t, err)

	config := &talosgenerator.TalosInDockerConfig{
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

func TestTalosInDockerGenerator_Generate_OverwritesWithForce(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	gen := talosgenerator.NewTalosInDockerGenerator()

	// Create an existing .gitkeep with custom content
	clusterDir := filepath.Join(tempDir, "talos", "cluster")
	err := os.MkdirAll(clusterDir, 0o750)
	require.NoError(t, err)

	gitkeepPath := filepath.Join(clusterDir, ".gitkeep")
	err = os.WriteFile(gitkeepPath, []byte("existing content"), 0o600)
	require.NoError(t, err)

	config := &talosgenerator.TalosInDockerConfig{
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

func TestTalosInDockerGenerator_Generate_DisableDefaultCNI(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	gen := talosgenerator.NewTalosInDockerGenerator()

	config := &talosgenerator.TalosInDockerConfig{
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

func TestTalosInDockerGenerator_Generate_NoDisableCNIPatchWhenFalse(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	gen := talosgenerator.NewTalosInDockerGenerator()

	config := &talosgenerator.TalosInDockerConfig{
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
