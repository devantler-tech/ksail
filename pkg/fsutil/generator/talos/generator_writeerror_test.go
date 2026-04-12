package talosgenerator_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	talosgenerator "github.com/devantler-tech/ksail/v6/pkg/fsutil/generator/talos"
	yamlgenerator "github.com/devantler-tech/ksail/v6/pkg/fsutil/generator/yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGenerate_AllowSchedulingWriteError verifies that an os.WriteFile error
// in the allow-scheduling patch is propagated.
func TestGenerate_AllowSchedulingWriteError(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("permission semantics differ on Windows")
	}

	if os.Getuid() == 0 {
		t.Skip("running as root — permission checks are bypassed")
	}

	tempDir := t.TempDir()
	gen := talosgenerator.NewGenerator()

	config := &talosgenerator.Config{
		PatchesDir:  "talos",
		WorkerNodes: 0, // triggers allow-scheduling patch
	}

	// First pass: create the directory structure
	_, err := gen.Generate(config, yamlgenerator.Options{Output: tempDir, Force: true})
	require.NoError(t, err)

	// Make the cluster directory read-only to trigger WriteFile errors
	clusterDir := filepath.Join(tempDir, "talos", "cluster")

	// Remove all existing patch files first
	entries, err := os.ReadDir(clusterDir)
	require.NoError(t, err)
	for _, e := range entries {
		_ = os.Remove(filepath.Join(clusterDir, e.Name()))
	}

	err = os.Chmod(clusterDir, 0o555)
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Chmod(clusterDir, 0o755) })

	// Second pass: should fail writing the patch
	_, err = gen.Generate(config, yamlgenerator.Options{Output: tempDir, Force: true})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "allow-scheduling-on-control-planes")
}

// TestGenerate_DisableCNIWriteError verifies that an os.WriteFile error
// in the disable-cni patch is propagated.
func TestGenerate_DisableCNIWriteError(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("permission semantics differ on Windows")
	}

	if os.Getuid() == 0 {
		t.Skip("running as root — permission checks are bypassed")
	}

	tempDir := t.TempDir()
	gen := talosgenerator.NewGenerator()

	config := &talosgenerator.Config{
		PatchesDir:        "talos",
		WorkerNodes:       1,
		DisableDefaultCNI: true,
	}

	// First pass: create directory structure
	_, err := gen.Generate(config, yamlgenerator.Options{Output: tempDir, Force: true})
	require.NoError(t, err)

	// Remove patch and make directory read-only
	clusterDir := filepath.Join(tempDir, "talos", "cluster")

	entries, err := os.ReadDir(clusterDir)
	require.NoError(t, err)
	for _, e := range entries {
		_ = os.Remove(filepath.Join(clusterDir, e.Name()))
	}

	err = os.Chmod(clusterDir, 0o555)
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Chmod(clusterDir, 0o755) })

	_, err = gen.Generate(config, yamlgenerator.Options{Output: tempDir, Force: true})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "disable-default-cni")
}

// TestGenerate_MirrorRegistriesWriteError verifies that an os.WriteFile error
// in the mirror registries patch is propagated.
func TestGenerate_MirrorRegistriesWriteError(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("permission semantics differ on Windows")
	}

	if os.Getuid() == 0 {
		t.Skip("running as root — permission checks are bypassed")
	}

	tempDir := t.TempDir()
	gen := talosgenerator.NewGenerator()

	config := &talosgenerator.Config{
		PatchesDir:       "talos",
		WorkerNodes:      1,
		MirrorRegistries: []string{"docker.io=http://mirror.local:5000"},
	}

	// First pass: create directory structure
	_, err := gen.Generate(config, yamlgenerator.Options{Output: tempDir, Force: true})
	require.NoError(t, err)

	// Remove patch and make directory read-only
	clusterDir := filepath.Join(tempDir, "talos", "cluster")

	entries, err := os.ReadDir(clusterDir)
	require.NoError(t, err)
	for _, e := range entries {
		_ = os.Remove(filepath.Join(clusterDir, e.Name()))
	}

	err = os.Chmod(clusterDir, 0o555)
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Chmod(clusterDir, 0o755) })

	_, err = gen.Generate(config, yamlgenerator.Options{Output: tempDir, Force: true})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "mirror registries")
}

// TestGenerate_KubeletCertRotationWriteError verifies that an os.WriteFile
// error in the kubelet cert rotation patch is propagated.
func TestGenerate_KubeletCertRotationWriteError(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("permission semantics differ on Windows")
	}

	if os.Getuid() == 0 {
		t.Skip("running as root — permission checks are bypassed")
	}

	tempDir := t.TempDir()
	gen := talosgenerator.NewGenerator()

	config := &talosgenerator.Config{
		PatchesDir:                "talos",
		WorkerNodes:               1,
		EnableKubeletCertRotation: true,
	}

	// First pass: create directory structure
	_, err := gen.Generate(config, yamlgenerator.Options{Output: tempDir, Force: true})
	require.NoError(t, err)

	// Remove patch and make directory read-only
	clusterDir := filepath.Join(tempDir, "talos", "cluster")

	entries, err := os.ReadDir(clusterDir)
	require.NoError(t, err)
	for _, e := range entries {
		_ = os.Remove(filepath.Join(clusterDir, e.Name()))
	}

	err = os.Chmod(clusterDir, 0o555)
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Chmod(clusterDir, 0o755) })

	_, err = gen.Generate(config, yamlgenerator.Options{Output: tempDir, Force: true})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "kubelet-cert-rotation")
}

// TestGenerate_ClusterNameWriteError verifies that an os.WriteFile error
// in the cluster name patch is propagated.
func TestGenerate_ClusterNameWriteError(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("permission semantics differ on Windows")
	}

	if os.Getuid() == 0 {
		t.Skip("running as root — permission checks are bypassed")
	}

	tempDir := t.TempDir()
	gen := talosgenerator.NewGenerator()

	config := &talosgenerator.Config{
		PatchesDir:  "talos",
		WorkerNodes: 1,
		ClusterName: "my-cluster",
	}

	// First pass
	_, err := gen.Generate(config, yamlgenerator.Options{Output: tempDir, Force: true})
	require.NoError(t, err)

	clusterDir := filepath.Join(tempDir, "talos", "cluster")

	entries, err := os.ReadDir(clusterDir)
	require.NoError(t, err)
	for _, e := range entries {
		_ = os.Remove(filepath.Join(clusterDir, e.Name()))
	}

	err = os.Chmod(clusterDir, 0o555)
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Chmod(clusterDir, 0o755) })

	_, err = gen.Generate(config, yamlgenerator.Options{Output: tempDir, Force: true})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "cluster-name")
}

// TestGenerate_ImageVerificationWriteError verifies that an os.WriteFile error
// in the image verification patch is propagated.
func TestGenerate_ImageVerificationWriteError(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("permission semantics differ on Windows")
	}

	if os.Getuid() == 0 {
		t.Skip("running as root — permission checks are bypassed")
	}

	tempDir := t.TempDir()
	gen := talosgenerator.NewGenerator()

	config := &talosgenerator.Config{
		PatchesDir:              "talos",
		WorkerNodes:             1,
		EnableImageVerification: true,
	}

	_, err := gen.Generate(config, yamlgenerator.Options{Output: tempDir, Force: true})
	require.NoError(t, err)

	clusterDir := filepath.Join(tempDir, "talos", "cluster")

	entries, err := os.ReadDir(clusterDir)
	require.NoError(t, err)
	for _, e := range entries {
		_ = os.Remove(filepath.Join(clusterDir, e.Name()))
	}

	err = os.Chmod(clusterDir, 0o555)
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Chmod(clusterDir, 0o755) })

	_, err = gen.Generate(config, yamlgenerator.Options{Output: tempDir, Force: true})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "image-verification")
}

// TestGenerate_DisableCDIWriteError verifies that an os.WriteFile error
// in the disable-cdi patch is propagated.
func TestGenerate_DisableCDIWriteError(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("permission semantics differ on Windows")
	}

	if os.Getuid() == 0 {
		t.Skip("running as root — permission checks are bypassed")
	}

	tempDir := t.TempDir()
	gen := talosgenerator.NewGenerator()

	config := &talosgenerator.Config{
		PatchesDir:  "talos",
		WorkerNodes: 1,
		DisableCDI:  true,
	}

	_, err := gen.Generate(config, yamlgenerator.Options{Output: tempDir, Force: true})
	require.NoError(t, err)

	clusterDir := filepath.Join(tempDir, "talos", "cluster")

	entries, err := os.ReadDir(clusterDir)
	require.NoError(t, err)
	for _, e := range entries {
		_ = os.Remove(filepath.Join(clusterDir, e.Name()))
	}

	err = os.Chmod(clusterDir, 0o555)
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.Chmod(clusterDir, 0o755) })

	_, err = gen.Generate(config, yamlgenerator.Options{Output: tempDir, Force: true})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "disable-cdi")
}
