package helm_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestUserSuppliedValues_MergesInlineSourcesLikeInstall pins the merge order
// Install and Upgrade use: values YAML first, then --set entries, then
// --set-json entries, each overriding the previous one. Drift detection
// compares this result with a release's stored values, so a different order
// would report drift on a release that matches.
func TestUserSuppliedValues_MergesInlineSourcesLikeInstall(t *testing.T) {
	t.Parallel()

	values, err := helm.UserSuppliedValues(&helm.ChartSpec{
		ValuesYaml: "replicaCount: 1\nimage:\n  tag: v1\n  pullPolicy: IfNotPresent\n",
		SetValues: map[string]string{
			"replicaCount": "2",
			"installCRDs":  "true",
			"image.tag":    "v2",
		},
		SetJSONVals: map[string]string{
			"image.tag": `"v3"`,
		},
	})
	require.NoError(t, err)

	assert.Equal(t, map[string]any{
		"replicaCount": int64(2),
		"installCRDs":  true,
		"image": map[string]any{
			"tag":        "v3",
			"pullPolicy": "IfNotPresent",
		},
	}, values)
}

// TestUserSuppliedValues_EmptySpecRendersNoValues keeps a chart without
// KSail-supplied values comparable: it renders an empty, non-nil map.
func TestUserSuppliedValues_EmptySpecRendersNoValues(t *testing.T) {
	t.Parallel()

	values, err := helm.UserSuppliedValues(&helm.ChartSpec{})
	require.NoError(t, err)
	assert.NotNil(t, values)
	assert.Empty(t, values)
}

// TestUserSuppliedValues_RefusesChartRelativeFiles never guesses at values
// that resolve against the chart: without loading it, a file-backed source
// cannot be rendered, and leaving it out would understate the values.
func TestUserSuppliedValues_RefusesChartRelativeFiles(t *testing.T) {
	t.Parallel()

	for name, spec := range map[string]*helm.ChartSpec{
		"value files":     {ValueFiles: []string{"values.yaml"}},
		"set-file values": {SetFileVals: map[string]string{"config": "config.txt"}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := helm.UserSuppliedValues(spec)
			require.ErrorIs(t, err, helm.ErrChartRelativeValues)
		})
	}
}

// TestUserSuppliedValues_SurfacesInvalidValues reports malformed values
// instead of rendering a partial map.
func TestUserSuppliedValues_SurfacesInvalidValues(t *testing.T) {
	t.Parallel()

	_, err := helm.UserSuppliedValues(&helm.ChartSpec{ValuesYaml: "key: [unterminated"})
	require.Error(t, err)
}
