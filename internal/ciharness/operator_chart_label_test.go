package ciharness_test

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"helm.sh/helm/v4/pkg/chart/common"
	commonutil "helm.sh/helm/v4/pkg/chart/common/util"
	chartloader "helm.sh/helm/v4/pkg/chart/v2/loader"
	"helm.sh/helm/v4/pkg/engine"
	"k8s.io/apimachinery/pkg/util/validation"
)

// Flux appends the artifact digest to the chart version as SemVer build
// metadata when a HelmRelease takes its chart from an OCIRepository. The chart
// label must stay a valid label value for such a version, or the API server
// rejects every rendered object.
func TestOperatorChartLabelAcceptsVersionBuildMetadata(t *testing.T) {
	t.Parallel()

	chartLabel := regexp.MustCompile(`(?m)^\s*helm\.sh/chart:\s*(.*)$`)

	for name, version := range map[string]string{
		"flux digest suffix":  "7.202.28+b69a2c9f4e13",
		"long build metadata": "7.202.28+" + strings.Repeat("a", 80),
		// The 63-character cut lands inside a run of mixed punctuation.
		"punctuation at the cut": "7.202.28+" + strings.Repeat("a", 36) + "-.-." + strings.Repeat("b", 20),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			chart, err := chartloader.Load(filepath.Join("..", "..", "charts", "ksail-operator"))
			require.NoError(t, err)

			chart.Metadata.Version = version

			values, err := commonutil.ToRenderValues(
				chart,
				map[string]any{},
				common.ReleaseOptions{
					Name:      "ksail-operator",
					Namespace: "ksail-operator",
					Revision:  1, IsUpgrade: false, IsInstall: true,
				},
				nil,
			)
			require.NoError(t, err)

			rendered, err := engine.Render(chart, values)
			require.NoError(t, err)

			labels := 0

			for file, manifest := range rendered {
				for _, match := range chartLabel.FindAllStringSubmatch(manifest, -1) {
					labels++

					value := strings.Trim(strings.TrimSpace(match[1]), `"'`)
					assert.Empty(
						t,
						validation.IsValidLabelValue(value),
						"%s: helm.sh/chart=%q",
						file,
						value,
					)
					assert.True(
						t,
						strings.HasPrefix(value, "ksail-operator-7.202.28"),
						"%s: %q",
						file,
						value,
					)
				}
			}

			assert.Positive(t, labels, "the chart must still render its helm.sh/chart label")
		})
	}
}
