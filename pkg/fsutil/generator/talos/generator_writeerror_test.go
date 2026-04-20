package talosgenerator_test

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/devantler-tech/ksail/v7/internal/testutil/rootcheck"
	talosgenerator "github.com/devantler-tech/ksail/v7/pkg/fsutil/generator/talos"
	yamlgenerator "github.com/devantler-tech/ksail/v7/pkg/fsutil/generator/yaml"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const windowsOS = "windows"

func skipPermissionWriteFailureTest(t *testing.T) {
	t.Helper()

	if runtime.GOOS == windowsOS {
		t.Skip("permission semantics differ on Windows")
	}

	if rootcheck.IsRootUser() {
		t.Skip("running as root — permission checks are bypassed")
	}
}

func setClusterDirReadOnly(path string) error {
	//nolint:gosec // Test intentionally toggles directory permissions to simulate write failures.
	err := os.Chmod(path, 0o555)
	if err != nil {
		return fmt.Errorf("chmod %s read-only: %w", path, err)
	}

	return nil
}

func restoreClusterDirMode(path string, mode os.FileMode) {
	_ = os.Chmod(path, mode)
}

func prepareClusterDirForWriteFailure(t *testing.T, clusterDir string) {
	t.Helper()

	info, err := os.Stat(clusterDir)
	require.NoError(t, err)

	entries, err := os.ReadDir(clusterDir)
	require.NoError(t, err)

	for _, entry := range entries {
		require.NoError(t, os.RemoveAll(filepath.Join(clusterDir, entry.Name())))
	}

	require.NoError(t, setClusterDirReadOnly(clusterDir))
	t.Cleanup(func() { restoreClusterDirMode(clusterDir, info.Mode()) })
}

func assertGenerateWriteError(
	t *testing.T,
	config *talosgenerator.Config,
	wantErrorSubstring string,
) {
	t.Helper()

	skipPermissionWriteFailureTest(t)

	tempDir := t.TempDir()
	gen := talosgenerator.NewGenerator()

	_, err := gen.Generate(config, yamlgenerator.Options{Output: tempDir, Force: true})
	require.NoError(t, err)

	clusterDir := filepath.Join(tempDir, "talos", "cluster")
	prepareClusterDirForWriteFailure(t, clusterDir)

	_, err = gen.Generate(config, yamlgenerator.Options{Output: tempDir, Force: true})
	require.Error(t, err)
	require.ErrorIs(t, err, fs.ErrPermission, "expected permission-denied write failure")
	assert.Contains(t, err.Error(), wantErrorSubstring)
}

func TestGenerate_AllowSchedulingWriteError(t *testing.T) {
	t.Parallel()

	assertGenerateWriteError(t, &talosgenerator.Config{
		PatchesDir:  "talos",
		WorkerNodes: 0,
	}, "allow-scheduling-on-control-planes")
}

func TestGenerate_DisableCNIWriteError(t *testing.T) {
	t.Parallel()

	assertGenerateWriteError(t, &talosgenerator.Config{
		PatchesDir:        "talos",
		WorkerNodes:       1,
		DisableDefaultCNI: true,
	}, "disable-default-cni")
}

func TestGenerate_MirrorRegistriesWriteError(t *testing.T) {
	t.Parallel()

	assertGenerateWriteError(t, &talosgenerator.Config{
		PatchesDir:       "talos",
		WorkerNodes:      1,
		MirrorRegistries: []string{"docker.io=http://mirror.local:5000"},
	}, "mirror registries")
}

func TestGenerate_KubeletCertRotationWriteError(t *testing.T) {
	t.Parallel()

	assertGenerateWriteError(t, &talosgenerator.Config{
		PatchesDir:                "talos",
		WorkerNodes:               1,
		EnableKubeletCertRotation: true,
	}, "kubelet-cert-rotation")
}

func TestGenerate_ClusterNameWriteError(t *testing.T) {
	t.Parallel()

	assertGenerateWriteError(t, &talosgenerator.Config{
		PatchesDir:  "talos",
		WorkerNodes: 1,
		ClusterName: "my-cluster",
	}, "cluster-name")
}

func TestGenerate_ImageVerificationWriteError(t *testing.T) {
	t.Parallel()

	assertGenerateWriteError(t, &talosgenerator.Config{
		PatchesDir:              "talos",
		WorkerNodes:             1,
		EnableImageVerification: true,
	}, "image-verification")
}

func TestGenerate_DisableCDIWriteError(t *testing.T) {
	t.Parallel()

	assertGenerateWriteError(t, &talosgenerator.Config{
		PatchesDir:  "talos",
		WorkerNodes: 1,
		DisableCDI:  true,
	}, "disable-cdi")
}
