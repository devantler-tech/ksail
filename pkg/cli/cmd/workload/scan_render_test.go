package workload_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/workload"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newScanCmd returns a scan command with its output captured, for driving the
// exported scan helpers in tests.
func newScanCmd() *cobra.Command {
	cmd := workload.NewScanCmd()

	var output bytes.Buffer

	cmd.SetOut(&output)
	cmd.SetErr(&output)

	return cmd
}

// TestResolveScanInputRendersKustomization verifies that, by default, scan
// renders a kustomization root to a temp directory (containing the rendered
// manifests) rather than scanning the raw source, and that cleanup removes it.
func TestResolveScanInputRendersKustomization(t *testing.T) {
	t.Parallel()

	dir := writeHelmReleaseKustomization(t, localChartURL(t, "validchart"))

	scanPath, cleanup, err := workload.ExportResolveScanInput(
		context.Background(), newScanCmd(), dir, nil, false, false,
	)
	require.NoError(t, err)
	require.NotEqual(t, dir, scanPath, "render should target a temp dir, not the source")

	manifestPath := filepath.Join(scanPath, "manifests.yaml")

	data, readErr := os.ReadFile(manifestPath) //nolint:gosec // test temp path
	require.NoError(t, readErr)
	assert.Contains(t, string(data), "kind: ConfigMap")
	assert.Contains(t, string(data), "app-config")

	cleanup()

	_, statErr := os.Stat(scanPath)
	assert.True(t, os.IsNotExist(statErr), "cleanup should remove the temp dir")
}

// TestResolveScanInputRawSkipsRender verifies --raw scans the source path
// directly (the pre-rendering behavior).
func TestResolveScanInputRawSkipsRender(t *testing.T) {
	t.Parallel()

	dir := writeHelmReleaseKustomization(t, localChartURL(t, "validchart"))

	scanPath, cleanup, err := workload.ExportResolveScanInput(
		context.Background(), newScanCmd(), dir, nil, false, true,
	)
	require.NoError(t, err)

	defer cleanup()

	assert.Equal(t, dir, scanPath, "--raw should scan the source path directly")
}

// TestResolveScanInputNoKustomizationSkipsRender verifies a directory without a
// kustomization root is scanned raw (nothing to render).
func TestResolveScanInputNoKustomizationSkipsRender(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "configmap.yaml"),
		[]byte("apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: plain\n"),
		0o600,
	))

	scanPath, cleanup, err := workload.ExportResolveScanInput(
		context.Background(), newScanCmd(), dir, nil, false, false,
	)
	require.NoError(t, err)

	defer cleanup()

	assert.Equal(t, dir, scanPath, "a directory without a kustomization is scanned raw")
}

// TestResolveScanInputHelmRenderDisabledByConfig verifies
// spec.workload.validation.helmRender:false disables scan rendering.
func TestResolveScanInputHelmRenderDisabledByConfig(t *testing.T) {
	t.Parallel()

	dir := writeHelmReleaseKustomization(t, localChartURL(t, "validchart"))

	disabled := false
	cfg := &v1alpha1.Cluster{}
	cfg.Spec.Workload.Validation.HelmRender = &disabled

	scanPath, cleanup, err := workload.ExportResolveScanInput(
		context.Background(), newScanCmd(), dir, cfg, true, false,
	)
	require.NoError(t, err)

	defer cleanup()

	assert.Equal(t, dir, scanPath, "helmRender: false disables scan rendering")
}
