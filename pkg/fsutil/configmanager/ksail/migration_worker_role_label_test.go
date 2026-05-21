package configmanager_test

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
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

func TestRemoveWorkerRoleLabelPatch_WarnsOnUnreadableFile(t *testing.T) {
	t.Parallel()

	patchesDir := t.TempDir()
	workersDir := filepath.Join(patchesDir, "workers")
	require.NoError(t, os.MkdirAll(workersDir, 0o750))

	// A symlink escaping patchesDir makes ReadFileSafe return ErrPathOutsideBase
	// (not os.ErrNotExist), which must produce a warning rather than a silent no-op.
	outside := filepath.Join(t.TempDir(), "target.yaml")
	require.NoError(t, os.WriteFile(outside, []byte("data"), 0o600))

	patchFile := filepath.Join(workersDir, "worker-role-label.yaml")

	err := os.Symlink(outside, patchFile)
	if err != nil {
		t.Skipf("symlinks not supported on this platform: %v", err)
	}

	var buf bytes.Buffer

	cm := &configmanager.ConfigManager{Writer: &buf}
	cm.RemoveWorkerRoleLabelPatchForTest(patchesDir)

	assert.Contains(
		t,
		buf.String(),
		"worker-role-label.yaml",
		"should warn when the file cannot be read safely",
	)

	// The symlink (and its target) must be left in place, not followed and deleted.
	_, err = os.Lstat(patchFile)
	require.NoError(t, err, "symlink should not be removed")
}

func TestRemoveWorkerRoleLabelPatch_WarnsWhenDeleteFails(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("directory write permissions are not enforced the same way on Windows")
	}

	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory write permissions, so os.Remove would not fail")
	}

	patchesDir, _ := writeWorkerRoleLabelPatch(
		t,
		configmanager.KubeletWorkerRoleLabelPatchYAMLForTest,
	)
	workersDir := filepath.Join(patchesDir, "workers")

	// Make the parent directory non-writable (but still readable/executable) so the
	// file can be read but os.Remove fails. Restore on cleanup so t.TempDir removal works.
	//nolint:gosec // directory needs the execute bit; permissions are restored in cleanup
	t.Cleanup(func() { _ = os.Chmod(workersDir, 0o700) })
	//nolint:gosec // 0o500 keeps the dir traversable but read-only to force os.Remove to fail
	require.NoError(t, os.Chmod(workersDir, 0o500))

	var buf bytes.Buffer

	cm := &configmanager.ConfigManager{Writer: &buf}
	cm.RemoveWorkerRoleLabelPatchForTest(patchesDir)

	assert.Contains(
		t,
		buf.String(),
		"could not delete",
		"should warn when the stale patch cannot be removed",
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
