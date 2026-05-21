package configmanager_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	configmanager "github.com/devantler-tech/ksail/v7/pkg/fsutil/configmanager/ksail"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeWorkerRoleLabelPatch(t *testing.T, content string) (string, string) {
	t.Helper()

	patchesDir := t.TempDir()
	workersDir := filepath.Join(patchesDir, "workers")
	require.NoError(t, os.MkdirAll(workersDir, 0o750))

	patchFile := filepath.Join(workersDir, "worker-role-label.yaml")
	require.NoError(t, os.WriteFile(patchFile, []byte(content), 0o600))

	return patchesDir, patchFile
}

func TestRemoveWorkerRoleLabelPatch_DeletesLegacyScaffold(t *testing.T) {
	t.Parallel()

	patchesDir, patchFile := writeWorkerRoleLabelPatch(
		t,
		configmanager.LegacyWorkerRoleLabelPatchYAMLForTest,
	)

	cm := &configmanager.ConfigManager{}
	cm.RemoveWorkerRoleLabelPatchForTest(patchesDir)

	_, err := os.Stat(patchFile)
	assert.True(t, os.IsNotExist(err), "legacy nodeLabels scaffold should be deleted")
}

func TestRemoveWorkerRoleLabelPatch_DeletesKubeletScaffold(t *testing.T) {
	t.Parallel()

	patchesDir, patchFile := writeWorkerRoleLabelPatch(
		t,
		configmanager.KubeletWorkerRoleLabelPatchYAMLForTest,
	)

	cm := &configmanager.ConfigManager{}
	cm.RemoveWorkerRoleLabelPatchForTest(patchesDir)

	_, err := os.Stat(patchFile)
	assert.True(t, os.IsNotExist(err), "kubelet --node-labels scaffold should be deleted")
}

func TestRemoveWorkerRoleLabelPatch_LeavesCustomizedFileAndWarns(t *testing.T) {
	t.Parallel()

	// File contains the worker role label plus additional user content, so it does not
	// match a known scaffold exactly and must not be deleted.
	customContent := "machine:\n  nodeLabels:\n" +
		"    node-role.kubernetes.io/worker: \"\"\n" +
		"    custom.label/app: \"myapp\"\n"
	patchesDir, patchFile := writeWorkerRoleLabelPatch(t, customContent)

	var buf bytes.Buffer

	cm := &configmanager.ConfigManager{Writer: &buf}
	cm.RemoveWorkerRoleLabelPatchForTest(patchesDir)

	got, err := os.ReadFile(patchFile) //nolint:gosec // test reads from t.TempDir
	require.NoError(t, err)
	assert.Equal(t, customContent, string(got), "customized file should be left untouched")
	assert.Contains(
		t,
		buf.String(),
		"node-role.kubernetes.io/worker",
		"should warn about the label",
	)
}

func TestRemoveWorkerRoleLabelPatch_NoFileIsNoop(t *testing.T) {
	t.Parallel()

	patchesDir := t.TempDir()
	// No workers/worker-role-label.yaml exists.

	cm := &configmanager.ConfigManager{}
	cm.RemoveWorkerRoleLabelPatchForTest(patchesDir)

	_, err := os.Stat(filepath.Join(patchesDir, "workers", "worker-role-label.yaml"))
	assert.True(t, os.IsNotExist(err), "no file should be created")
}
