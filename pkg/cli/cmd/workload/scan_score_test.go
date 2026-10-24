package workload_test

import (
	"bytes"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/workload"
	"github.com/stretchr/testify/assert"
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
