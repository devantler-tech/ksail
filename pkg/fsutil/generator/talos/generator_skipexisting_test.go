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

type skipExistingFile struct {
	relativePath string
	content      string
}

func assertGenerateSkipExistingPreservesContent(
	t *testing.T,
	config *talosgenerator.Config,
	files ...skipExistingFile,
) {
	t.Helper()

	tempDir := t.TempDir()
	gen := talosgenerator.NewGenerator()

	_, err := gen.Generate(config, yamlgenerator.Options{Output: tempDir, Force: true})
	require.NoError(t, err)

	for _, file := range files {
		patchPath := filepath.Join(tempDir, file.relativePath)

		err = os.WriteFile(patchPath, []byte(file.content), 0o600)
		require.NoError(t, err)
	}

	_, err = gen.Generate(config, yamlgenerator.Options{Output: tempDir, Force: false})
	require.NoError(t, err)

	for _, file := range files {
		patchPath := filepath.Join(tempDir, file.relativePath)

		//nolint:gosec // Test reads a file created in its own temp directory.
		content, err := os.ReadFile(patchPath)
		require.NoError(t, err)
		assert.Equal(t, file.content, string(content))
	}
}

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

	assertGenerateSkipExistingPreservesContent(t, &talosgenerator.Config{
		PatchesDir:       "talos",
		WorkerNodes:      1,
		MirrorRegistries: []string{"docker.io=http://mirror.local:5000"},
	}, skipExistingFile{
		relativePath: filepath.Join("talos", "cluster", "mirror-registries.yaml"),
		content:      "custom-content",
	})
}

// TestGenerate_AllowSchedulingSkipExisting verifies that the allow-scheduling
// patch is not overwritten when force is false.
func TestGenerate_AllowSchedulingSkipExisting(t *testing.T) {
	t.Parallel()

	assertGenerateSkipExistingPreservesContent(t, &talosgenerator.Config{
		PatchesDir:  "talos",
		WorkerNodes: 0, // triggers allow-scheduling patch
	}, skipExistingFile{
		relativePath: filepath.Join("talos", "cluster", "allow-scheduling-on-control-planes.yaml"),
		content:      "original",
	})
}

// TestGenerate_DisableCNISkipExisting verifies that the disable-cni patch is
// not overwritten when force is false.
func TestGenerate_DisableCNISkipExisting(t *testing.T) {
	t.Parallel()

	assertGenerateSkipExistingPreservesContent(t, &talosgenerator.Config{
		PatchesDir:        "talos",
		WorkerNodes:       1,
		DisableDefaultCNI: true,
	}, skipExistingFile{
		relativePath: filepath.Join("talos", "cluster", "disable-default-cni.yaml"),
		content:      "original",
	})
}

// TestGenerate_KubeletCertRotationSkipExisting verifies that kubelet cert
// rotation patches are not overwritten when force is false.
func TestGenerate_KubeletCertRotationSkipExisting(t *testing.T) {
	t.Parallel()

	assertGenerateSkipExistingPreservesContent(t, &talosgenerator.Config{
		PatchesDir:                "talos",
		WorkerNodes:               1,
		EnableKubeletCertRotation: true,
	},
		skipExistingFile{
			relativePath: filepath.Join("talos", "cluster", "kubelet-cert-rotation.yaml"),
			content:      "original-cert",
		},
		skipExistingFile{
			relativePath: filepath.Join("talos", "cluster", "kubelet-csr-approver.yaml"),
			content:      "original-csr",
		},
	)
}

// TestGenerate_ClusterNameSkipExisting verifies that the cluster name patch is
// not overwritten when force is false.
func TestGenerate_ClusterNameSkipExisting(t *testing.T) {
	t.Parallel()

	assertGenerateSkipExistingPreservesContent(t, &talosgenerator.Config{
		PatchesDir:  "talos",
		WorkerNodes: 1,
		ClusterName: "my-cluster",
	}, skipExistingFile{
		relativePath: filepath.Join("talos", "cluster", "cluster-name.yaml"),
		content:      "original",
	})
}

// TestGenerate_ImageVerificationSkipExisting verifies that the image verification
// patch is not overwritten when force is false.
func TestGenerate_ImageVerificationSkipExisting(t *testing.T) {
	t.Parallel()

	assertGenerateSkipExistingPreservesContent(t, &talosgenerator.Config{
		PatchesDir:              "talos",
		WorkerNodes:             1,
		EnableImageVerification: true,
	}, skipExistingFile{
		relativePath: filepath.Join("talos", "cluster", "image-verification.yaml"),
		content:      "original",
	})
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
