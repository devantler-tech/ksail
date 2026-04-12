package talosgenerator_test

import (
	"os"
	"path/filepath"
	"testing"

	talosgenerator "github.com/devantler-tech/ksail/v6/pkg/fsutil/generator/talos"
	yamlgenerator "github.com/devantler-tech/ksail/v6/pkg/fsutil/generator/yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGenerate_NilConfig verifies that a nil config returns ErrConfigRequired.
func TestGenerate_NilConfig(t *testing.T) {
	t.Parallel()

	gen := talosgenerator.NewGenerator()

	_, err := gen.Generate(nil, yamlgenerator.Options{})

	require.Error(t, err)
	assert.ErrorIs(t, err, talosgenerator.ErrConfigRequired)
}

// TestGenerate_EmptyPatchesDir verifies that an empty PatchesDir defaults to "talos".
func TestGenerate_EmptyPatchesDir(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	gen := talosgenerator.NewGenerator()

	config := &talosgenerator.Config{
		PatchesDir:  "",
		WorkerNodes: 1,
	}
	opts := yamlgenerator.Options{
		Output: tempDir,
		Force:  true,
	}

	result, err := gen.Generate(config, opts)
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(tempDir, "talos"), result)
}

// TestGenerate_MirrorRegistriesSkipExisting verifies that mirror patch is not
// overwritten when force is false and file already exists.
func TestGenerate_MirrorRegistriesSkipExisting(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	gen := talosgenerator.NewGenerator()

	config := &talosgenerator.Config{
		PatchesDir:       "talos",
		WorkerNodes:      1,
		MirrorRegistries: []string{"docker.io=http://mirror.local:5000"},
	}

	// First generate with force to create the file
	_, err := gen.Generate(config, yamlgenerator.Options{Output: tempDir, Force: true})
	require.NoError(t, err)

	// Overwrite with custom content
	patchPath := filepath.Join(tempDir, "talos", "cluster", "mirror-registries.yaml")
	err = os.WriteFile(patchPath, []byte("custom-content"), 0o600)
	require.NoError(t, err)

	// Generate again without force — should not overwrite
	_, err = gen.Generate(config, yamlgenerator.Options{Output: tempDir, Force: false})
	require.NoError(t, err)

	content, err := os.ReadFile(patchPath)
	require.NoError(t, err)
	assert.Equal(t, "custom-content", string(content))
}

// TestGenerate_AllowSchedulingSkipExisting verifies that the allow-scheduling
// patch is not overwritten when force is false.
func TestGenerate_AllowSchedulingSkipExisting(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	gen := talosgenerator.NewGenerator()

	config := &talosgenerator.Config{
		PatchesDir:  "talos",
		WorkerNodes: 0, // triggers allow-scheduling patch
	}

	// First generate with force
	_, err := gen.Generate(config, yamlgenerator.Options{Output: tempDir, Force: true})
	require.NoError(t, err)

	// Overwrite with custom content
	patchPath := filepath.Join(tempDir, "talos", "cluster", "allow-scheduling-on-control-planes.yaml")
	err = os.WriteFile(patchPath, []byte("original"), 0o600)
	require.NoError(t, err)

	// Generate again without force
	_, err = gen.Generate(config, yamlgenerator.Options{Output: tempDir, Force: false})
	require.NoError(t, err)

	content, err := os.ReadFile(patchPath)
	require.NoError(t, err)
	assert.Equal(t, "original", string(content))
}

// TestGenerate_DisableCNISkipExisting verifies that the disable-cni patch is
// not overwritten when force is false.
func TestGenerate_DisableCNISkipExisting(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	gen := talosgenerator.NewGenerator()

	config := &talosgenerator.Config{
		PatchesDir:        "talos",
		WorkerNodes:       1,
		DisableDefaultCNI: true,
	}

	// First generate with force
	_, err := gen.Generate(config, yamlgenerator.Options{Output: tempDir, Force: true})
	require.NoError(t, err)

	// Overwrite with custom content
	patchPath := filepath.Join(tempDir, "talos", "cluster", "disable-default-cni.yaml")
	err = os.WriteFile(patchPath, []byte("original"), 0o600)
	require.NoError(t, err)

	// Generate again without force
	_, err = gen.Generate(config, yamlgenerator.Options{Output: tempDir, Force: false})
	require.NoError(t, err)

	content, err := os.ReadFile(patchPath)
	require.NoError(t, err)
	assert.Equal(t, "original", string(content))
}

// TestGenerate_KubeletCertRotationSkipExisting verifies that kubelet cert
// rotation patches are not overwritten when force is false.
func TestGenerate_KubeletCertRotationSkipExisting(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	gen := talosgenerator.NewGenerator()

	config := &talosgenerator.Config{
		PatchesDir:                "talos",
		WorkerNodes:               1,
		EnableKubeletCertRotation: true,
	}

	// First generate with force
	_, err := gen.Generate(config, yamlgenerator.Options{Output: tempDir, Force: true})
	require.NoError(t, err)

	// Overwrite with custom content
	certPath := filepath.Join(tempDir, "talos", "cluster", "kubelet-cert-rotation.yaml")
	csrPath := filepath.Join(tempDir, "talos", "cluster", "kubelet-csr-approver.yaml")

	err = os.WriteFile(certPath, []byte("original-cert"), 0o600)
	require.NoError(t, err)

	err = os.WriteFile(csrPath, []byte("original-csr"), 0o600)
	require.NoError(t, err)

	// Generate again without force
	_, err = gen.Generate(config, yamlgenerator.Options{Output: tempDir, Force: false})
	require.NoError(t, err)

	certContent, err := os.ReadFile(certPath)
	require.NoError(t, err)
	assert.Equal(t, "original-cert", string(certContent))

	csrContent, err := os.ReadFile(csrPath)
	require.NoError(t, err)
	assert.Equal(t, "original-csr", string(csrContent))
}

// TestGenerate_ClusterNameSkipExisting verifies that the cluster name patch is
// not overwritten when force is false.
func TestGenerate_ClusterNameSkipExisting(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	gen := talosgenerator.NewGenerator()

	config := &talosgenerator.Config{
		PatchesDir:  "talos",
		WorkerNodes: 1,
		ClusterName: "my-cluster",
	}

	// First generate with force
	_, err := gen.Generate(config, yamlgenerator.Options{Output: tempDir, Force: true})
	require.NoError(t, err)

	// Overwrite
	patchPath := filepath.Join(tempDir, "talos", "cluster", "cluster-name.yaml")
	err = os.WriteFile(patchPath, []byte("original"), 0o600)
	require.NoError(t, err)

	// Generate again without force
	_, err = gen.Generate(config, yamlgenerator.Options{Output: tempDir, Force: false})
	require.NoError(t, err)

	content, err := os.ReadFile(patchPath)
	require.NoError(t, err)
	assert.Equal(t, "original", string(content))
}

// TestGenerate_ImageVerificationSkipExisting verifies that the image verification
// patch is not overwritten when force is false.
func TestGenerate_ImageVerificationSkipExisting(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	gen := talosgenerator.NewGenerator()

	config := &talosgenerator.Config{
		PatchesDir:              "talos",
		WorkerNodes:             1,
		EnableImageVerification: true,
	}

	// First generate with force
	_, err := gen.Generate(config, yamlgenerator.Options{Output: tempDir, Force: true})
	require.NoError(t, err)

	// Overwrite
	patchPath := filepath.Join(tempDir, "talos", "cluster", "image-verification.yaml")
	err = os.WriteFile(patchPath, []byte("original"), 0o600)
	require.NoError(t, err)

	// Generate again without force
	_, err = gen.Generate(config, yamlgenerator.Options{Output: tempDir, Force: false})
	require.NoError(t, err)

	content, err := os.ReadFile(patchPath)
	require.NoError(t, err)
	assert.Equal(t, "original", string(content))
}

// TestGenerate_GitkeepExistingNoForce verifies that .gitkeep files are not
// overwritten when force is false.
func TestGenerate_GitkeepExistingNoForce(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	gen := talosgenerator.NewGenerator()

	// Config with only workers=1 so cluster/ gets .gitkeep
	config := &talosgenerator.Config{
		PatchesDir:  "talos",
		WorkerNodes: 1,
	}

	// First generate with force
	_, err := gen.Generate(config, yamlgenerator.Options{Output: tempDir, Force: true})
	require.NoError(t, err)

	// Verify .gitkeep exists in cluster/ (no patches there)
	gitkeepPath := filepath.Join(tempDir, "talos", "cluster", ".gitkeep")
	_, err = os.Stat(gitkeepPath)
	require.NoError(t, err)

	// Generate again without force — should not fail
	_, err = gen.Generate(config, yamlgenerator.Options{Output: tempDir, Force: false})
	require.NoError(t, err)
}
