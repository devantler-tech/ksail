package configmanager_test

import (
	"os"
	"path/filepath"
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

func TestAddWorkerRoleLabelPatch_PreservesCustomizedFile(t *testing.T) {
	t.Parallel()

	patchesDir := t.TempDir()
	workersDir := filepath.Join(patchesDir, "workers")
	require.NoError(t, os.MkdirAll(workersDir, 0o750))

	// File contains additional user content beyond the legacy scaffold.
	customContent := "machine:\n  nodeLabels:\n" +
		"    node-role.kubernetes.io/worker: \"\"\n" +
		"    custom.label/app: \"myapp\"\n"
	patchFile := filepath.Join(workersDir, "worker-role-label.yaml")
	require.NoError(t, os.WriteFile(patchFile, []byte(customContent), 0o600))

	cm := &configmanager.ConfigManager{}
	talosManager := talosconfigmanager.NewConfigManager(patchesDir, "test", "", "")

	cm.AddWorkerRoleLabelPatchForTest(talosManager, patchesDir)

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

	// The runtime patch should have been injected (file still absent).
	_, err := os.Stat(filepath.Join(patchesDir, "workers", "worker-role-label.yaml"))
	assert.True(t, os.IsNotExist(err), "file should not be created by runtime injection")
}
