package configmanager_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	configmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/ksail"
	talosconfigmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/talos"
	talosgenerator "github.com/devantler-tech/ksail/v7/pkg/fsutil/generator/talos"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAddWorkerRoleLabelPatch_MigratesLegacyScaffold(t *testing.T) {
	t.Parallel()

	patchesDir := t.TempDir()
	workersDir := filepath.Join(patchesDir, "workers")
	require.NoError(t, os.MkdirAll(workersDir, 0o750))

	patchFile := filepath.Join(workersDir, "worker-role-label.yaml")
	require.NoError(t, os.WriteFile(
		patchFile,
		[]byte(configmanager.LegacyWorkerRoleLabelPatchYAMLForTest),
		0o600,
	))

	cm := &configmanager.ConfigManager{}
	talosManager := talosconfigmanager.NewConfigManager(patchesDir, "test", "", "")

	cm.AddWorkerRoleLabelPatchForTest(talosManager, patchesDir)

	got, err := os.ReadFile(patchFile) //nolint:gosec // test reads from t.TempDir
	require.NoError(t, err)
	assert.Equal(t, talosgenerator.WorkerRoleLabelPatchYAML, string(got))
}

func TestAddWorkerRoleLabelPatch_CustomizedFileInjectsRuntime(t *testing.T) {
	t.Parallel()

	patchesDir := t.TempDir()
	workersDir := filepath.Join(patchesDir, "workers")
	require.NoError(t, os.MkdirAll(workersDir, 0o750))

	// File contains the legacy worker role label plus additional user labels.
	customContent := "machine:\n  nodeLabels:\n" +
		"    node-role.kubernetes.io/worker: \"\"\n" +
		"    custom.label/app: \"myapp\"\n"
	patchFile := filepath.Join(workersDir, "worker-role-label.yaml")
	require.NoError(t, os.WriteFile(patchFile, []byte(customContent), 0o600))

	cm := &configmanager.ConfigManager{}
	talosManager := talosconfigmanager.NewConfigManager(patchesDir, "test", "", "")

	cm.AddWorkerRoleLabelPatchForTest(talosManager, patchesDir)

	// File should be preserved (not overwritten).
	got, err := os.ReadFile(patchFile) //nolint:gosec // test reads from t.TempDir
	require.NoError(t, err)
	assert.Equal(t, customContent, string(got), "customized file should be left untouched")
}

func TestAddWorkerRoleLabelPatch_InjectsRuntimeWhenFileAbsent(t *testing.T) {
	t.Parallel()

	patchesDir := t.TempDir()
	// No workers/worker-role-label.yaml exists.

	cm := &configmanager.ConfigManager{}
	talosManager := talosconfigmanager.NewConfigManager(patchesDir, "test", "", "")

	cm.AddWorkerRoleLabelPatchForTest(talosManager, patchesDir)

	// The file should remain absent — runtime injection does not write to disk.
	_, err := os.Stat(filepath.Join(patchesDir, "workers", "worker-role-label.yaml"))
	assert.True(t, os.IsNotExist(err), "file should not be created by runtime injection")
}

func TestAddWorkerRoleLabelPatch_MergesExistingNodeLabels(t *testing.T) {
	t.Parallel()

	patchesDir := t.TempDir()
	workersDir := filepath.Join(patchesDir, "workers")
	require.NoError(t, os.MkdirAll(workersDir, 0o750))

	// Create a longhorn-style patch with custom node-labels.
	longhornPatch := "machine:\n  kubelet:\n    extraArgs:\n      node-labels: node.longhorn.io/create-default-disk=true\n"
	require.NoError(t, os.WriteFile(
		filepath.Join(workersDir, "longhorn.yaml"),
		[]byte(longhornPatch),
		0o600,
	))

	// No worker-role-label.yaml exists → runtime injection.
	cm := &configmanager.ConfigManager{}
	talosManager := talosconfigmanager.NewConfigManager(patchesDir, "test", "", "")

	cm.AddWorkerRoleLabelPatchForTest(talosManager, patchesDir)

	// Verify that the merged YAML includes both labels.
	merged := configmanager.MergedWorkerRoleLabelPatchYAMLForTest(patchesDir)
	assert.Contains(t, merged, "node-role.kubernetes.io/worker=")
	assert.Contains(t, merged, "node.longhorn.io/create-default-disk=true")
}

func TestAddWorkerRoleLabelPatch_LegacyMigrationMergesLabels(t *testing.T) {
	t.Parallel()

	patchesDir := t.TempDir()
	workersDir := filepath.Join(patchesDir, "workers")
	require.NoError(t, os.MkdirAll(workersDir, 0o750))

	// Create a longhorn-style patch with custom node-labels.
	longhornPatch := "machine:\n  kubelet:\n    extraArgs:\n      node-labels: node.longhorn.io/create-default-disk=true\n"
	require.NoError(t, os.WriteFile(
		filepath.Join(workersDir, "longhorn.yaml"),
		[]byte(longhornPatch),
		0o600,
	))

	// Create a legacy worker-role-label.yaml.
	patchFile := filepath.Join(workersDir, "worker-role-label.yaml")
	require.NoError(t, os.WriteFile(
		patchFile,
		[]byte(configmanager.LegacyWorkerRoleLabelPatchYAMLForTest),
		0o600,
	))

	cm := &configmanager.ConfigManager{}
	talosManager := talosconfigmanager.NewConfigManager(patchesDir, "test", "", "")

	cm.AddWorkerRoleLabelPatchForTest(talosManager, patchesDir)

	// After migration, the file should contain combined labels.
	got, err := os.ReadFile(patchFile) //nolint:gosec // test reads from t.TempDir
	require.NoError(t, err)
	assert.Contains(t, string(got), "node-role.kubernetes.io/worker=")
	assert.Contains(t, string(got), "node.longhorn.io/create-default-disk=true")
}

func TestCollectWorkerNodeLabelsFromPatches(t *testing.T) {
	t.Parallel()

	t.Run("no workers dir", func(t *testing.T) {
		t.Parallel()

		labels := configmanager.CollectWorkerNodeLabelsFromPatchesForTest(t.TempDir())
		assert.Empty(t, labels)
	})

	t.Run("single file with node-labels", func(t *testing.T) {
		t.Parallel()

		patchesDir := t.TempDir()
		workersDir := filepath.Join(patchesDir, "workers")
		require.NoError(t, os.MkdirAll(workersDir, 0o750))

		patch := "machine:\n  kubelet:\n    extraArgs:\n      node-labels: custom.io/label=value\n"
		require.NoError(t, os.WriteFile(filepath.Join(workersDir, "custom.yaml"), []byte(patch), 0o600))

		labels := configmanager.CollectWorkerNodeLabelsFromPatchesForTest(patchesDir)
		assert.Equal(t, []string{"custom.io/label=value"}, labels)
	})

	t.Run("multiple comma-separated labels", func(t *testing.T) {
		t.Parallel()

		patchesDir := t.TempDir()
		workersDir := filepath.Join(patchesDir, "workers")
		require.NoError(t, os.MkdirAll(workersDir, 0o750))

		patch := "machine:\n  kubelet:\n    extraArgs:\n      node-labels: \"a=1,b=2\"\n"
		require.NoError(t, os.WriteFile(filepath.Join(workersDir, "multi.yaml"), []byte(patch), 0o600))

		labels := configmanager.CollectWorkerNodeLabelsFromPatchesForTest(patchesDir)
		assert.Equal(t, []string{"a=1", "b=2"}, labels)
	})

	t.Run("excludes worker-role-label.yaml", func(t *testing.T) {
		t.Parallel()

		patchesDir := t.TempDir()
		workersDir := filepath.Join(patchesDir, "workers")
		require.NoError(t, os.MkdirAll(workersDir, 0o750))

		// This file should be excluded from scanning.
		excluded := "machine:\n  kubelet:\n    extraArgs:\n      node-labels: \"node-role.kubernetes.io/worker=\"\n"
		require.NoError(t, os.WriteFile(filepath.Join(workersDir, "worker-role-label.yaml"), []byte(excluded), 0o600))

		labels := configmanager.CollectWorkerNodeLabelsFromPatchesForTest(patchesDir)
		assert.Empty(t, labels)
	})

	t.Run("file without node-labels", func(t *testing.T) {
		t.Parallel()

		patchesDir := t.TempDir()
		workersDir := filepath.Join(patchesDir, "workers")
		require.NoError(t, os.MkdirAll(workersDir, 0o750))

		patch := "machine:\n  kubelet:\n    extraMounts:\n      - destination: /var/lib/data\n"
		require.NoError(t, os.WriteFile(filepath.Join(workersDir, "mounts.yaml"), []byte(patch), 0o600))

		labels := configmanager.CollectWorkerNodeLabelsFromPatchesForTest(patchesDir)
		assert.Empty(t, labels)
	})
}

func TestMergedWorkerRoleLabelPatchYAML(t *testing.T) {
	t.Parallel()

	t.Run("no existing labels uses standard constant", func(t *testing.T) {
		t.Parallel()

		patchesDir := t.TempDir()
		require.NoError(t, os.MkdirAll(filepath.Join(patchesDir, "workers"), 0o750))

		got := configmanager.MergedWorkerRoleLabelPatchYAMLForTest(patchesDir)
		assert.Equal(t, talosgenerator.WorkerRoleLabelPatchYAML, got)
	})

	t.Run("merges with existing labels", func(t *testing.T) {
		t.Parallel()

		patchesDir := t.TempDir()
		workersDir := filepath.Join(patchesDir, "workers")
		require.NoError(t, os.MkdirAll(workersDir, 0o750))

		patch := "machine:\n  kubelet:\n    extraArgs:\n      node-labels: custom.io/disk=true\n"
		require.NoError(t, os.WriteFile(filepath.Join(workersDir, "storage.yaml"), []byte(patch), 0o600))

		got := configmanager.MergedWorkerRoleLabelPatchYAMLForTest(patchesDir)
		assert.Contains(t, got, "node-role.kubernetes.io/worker=")
		assert.Contains(t, got, "custom.io/disk=true")
	})

	t.Run("deduplicates worker role label", func(t *testing.T) {
		t.Parallel()

		patchesDir := t.TempDir()
		workersDir := filepath.Join(patchesDir, "workers")
		require.NoError(t, os.MkdirAll(workersDir, 0o750))

		// File already has the worker role label.
		patch := "machine:\n  kubelet:\n    extraArgs:\n      node-labels: \"node-role.kubernetes.io/worker=,custom=yes\"\n"
		require.NoError(t, os.WriteFile(filepath.Join(workersDir, "labels.yaml"), []byte(patch), 0o600))

		got := configmanager.MergedWorkerRoleLabelPatchYAMLForTest(patchesDir)
		// Should not duplicate the worker role label.
		assert.Equal(t, 1, strings.Count(got, "node-role.kubernetes.io/worker="))
		assert.Contains(t, got, "custom=yes")
	})
}
