package workload_test

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/workload"
	kubescapeclient "github.com/devantler-tech/ksail/v7/pkg/client/kubescape"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWriteComplianceScore pins the score line a passing scan prints, with and
// without a --compliance-threshold, so the score a threshold can be ratcheted to
// is visible on every successful run (ksail#6052).
func TestWriteComplianceScore(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name      string
		score     float32
		threshold float32
		want      string
	}{
		{
			name:      "with a threshold",
			score:     98.87,
			threshold: 85,
			want:      "✔ compliance score 98.87% (threshold 85.00%)\n",
		},
		{
			name:  "without a threshold",
			score: 72.5,
			want:  "ℹ compliance score 72.50%\n",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			var out bytes.Buffer

			workload.ExportWriteComplianceScore(&out, test.score, test.threshold)

			assert.Equal(t, test.want, out.String())
		})
	}
}

// TestScanCmdWritesAPassingScoreToStderrOnly runs the scan command on a passing scan and
// pins where its score goes: stderr, so a report on stdout (json, sarif, junit) stays
// parseable (ksail#6052).
//
//nolint:paralleltest // Swaps the scan command's shared scanner seam.
func TestScanCmdWritesAPassingScoreToStderrOnly(t *testing.T) {
	var scannedPath string

	restore := workload.ExportSetDirectoryScanner(func(
		_ context.Context,
		path string,
		opts *kubescapeclient.ScanOptions,
	) (*kubescapeclient.ScanResult, error) {
		scannedPath = path

		assert.InDelta(t, 10, opts.ComplianceThreshold, 0.001)

		return &kubescapeclient.ScanResult{ComplianceScore: 60}, nil
	})
	t.Cleanup(restore)

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(
		filepath.Join(dir, "deployment.yaml"),
		[]byte("apiVersion: apps/v1\nkind: Deployment\nmetadata:\n  name: demo\n"),
		0o600,
	))

	var stdout, stderr bytes.Buffer

	cmd := workload.NewScanCmd()
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{dir, "--no-render", "--compliance-threshold", "10"})

	require.NoError(t, cmd.Execute())

	assert.NotEmpty(t, scannedPath, "the scan must reach the scanner")
	assert.Contains(t, stderr.String(), "✔ compliance score 60.00% (threshold 10.00%)\n")
	assert.NotContains(t, stdout.String(), "compliance score")
}
