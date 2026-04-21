package helm_test

import (
	"maps"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMergeSetValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		setValues map[string]string
		base      map[string]any
		wantBase  map[string]any
		wantErr   bool
	}{
		{
			name:      "nil map is no-op",
			setValues: nil,
			base:      map[string]any{},
			wantBase:  map[string]any{},
		},
		{
			name:      "empty map is no-op",
			setValues: map[string]string{},
			base:      map[string]any{},
			wantBase:  map[string]any{},
		},
		{
			name:      "single value",
			setValues: map[string]string{"replicas": "3"},
			base:      map[string]any{},
			wantBase:  map[string]any{"replicas": int64(3)},
		},
		{
			name:      "nested value with dot notation",
			setValues: map[string]string{"image.tag": "latest"},
			base:      map[string]any{},
			wantBase: map[string]any{
				"image": map[string]any{"tag": "latest"},
			},
		},
		{
			name:      "overrides existing value",
			setValues: map[string]string{"replicas": "5"},
			base:      map[string]any{"replicas": int64(3)},
			wantBase:  map[string]any{"replicas": int64(5)},
		},
	}

	for index := range tests {
		tc := tests[index] //nolint:varnamelen

		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			base := copyMap(tc.base)
			err := helm.MergeSetValues(tc.setValues, base)

			if tc.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tc.wantBase, base)
			}
		})
	}
}

func TestMergeSetJSONValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		setJSONVals map[string]string
		base        map[string]any
		wantErr     bool
		checkKey    string
		checkVal    any
	}{
		{
			name:        "nil map is no-op",
			setJSONVals: nil,
			base:        map[string]any{},
		},
		{
			name:        "empty map is no-op",
			setJSONVals: map[string]string{},
			base:        map[string]any{},
		},
		{
			name:        "single JSON value",
			setJSONVals: map[string]string{"config": `{"enabled":true}`},
			base:        map[string]any{},
			checkKey:    "config",
		},
		{
			name:        "invalid JSON returns error",
			setJSONVals: map[string]string{"config": "{invalid-json"},
			base:        map[string]any{},
			wantErr:     true,
		},
	}

	for index := range tests {
		testCase := tests[index]

		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			base := copyMap(testCase.base)
			err := helm.MergeSetJSONValues(testCase.setJSONVals, base)

			if testCase.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "failed to parse JSON value")
			} else {
				require.NoError(t, err)

				if testCase.checkKey != "" {
					assert.Contains(t, base, testCase.checkKey)
				}
			}
		})
	}
}

//nolint:funlen,varnamelen // Table-driven coverage is long and short names keep the cases readable.
func TestMergeValuesYaml(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		valuesYaml string
		base       map[string]any
		wantBase   map[string]any
		wantErr    bool
	}{
		{
			name:       "empty string is no-op",
			valuesYaml: "",
			base:       map[string]any{},
			wantBase:   map[string]any{},
		},
		{
			name:       "simple yaml",
			valuesYaml: "replicas: 3\nimage: nginx",
			base:       map[string]any{},
			wantBase:   map[string]any{"replicas": float64(3), "image": "nginx"},
		},
		{
			name:       "nested yaml",
			valuesYaml: "server:\n  port: 8080",
			base:       map[string]any{},
			wantBase: map[string]any{
				"server": map[string]any{"port": float64(8080)},
			},
		},
		{
			name:       "overrides existing values",
			valuesYaml: "replicas: 5",
			base:       map[string]any{"replicas": float64(3)},
			wantBase:   map[string]any{"replicas": float64(5)},
		},
		{
			name:       "invalid yaml returns error",
			valuesYaml: "{\n  invalid yaml: [",
			base:       map[string]any{},
			wantErr:    true,
		},
	}

	for index := range tests {
		tc := tests[index]

		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			base := copyMap(tc.base)
			err := helm.MergeValuesYaml(tc.valuesYaml, base)

			if tc.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "failed to parse ValuesYaml")
			} else {
				require.NoError(t, err)
				assert.Equal(t, tc.wantBase, base)
			}
		})
	}
}

//nolint:funlen // Table-driven test coverage is naturally long.
func TestMergeMapsInto(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		dest     map[string]any
		src      map[string]any
		wantDest map[string]any
	}{
		{
			name:     "empty source into empty dest",
			dest:     map[string]any{},
			src:      map[string]any{},
			wantDest: map[string]any{},
		},
		{
			name:     "new keys are added",
			dest:     map[string]any{"a": "1"},
			src:      map[string]any{"b": "2"},
			wantDest: map[string]any{"a": "1", "b": "2"},
		},
		{
			name:     "scalar values are overwritten",
			dest:     map[string]any{"a": "1"},
			src:      map[string]any{"a": "2"},
			wantDest: map[string]any{"a": "2"},
		},
		{
			name: "nested maps are merged recursively",
			dest: map[string]any{
				"nested": map[string]any{"a": "1", "b": "2"},
			},
			src: map[string]any{
				"nested": map[string]any{"b": "3", "c": "4"},
			},
			wantDest: map[string]any{
				"nested": map[string]any{"a": "1", "b": "3", "c": "4"},
			},
		},
		{
			name: "src map overwrites dest scalar",
			dest: map[string]any{
				"key": "scalar",
			},
			src: map[string]any{
				"key": map[string]any{"nested": "value"},
			},
			wantDest: map[string]any{
				"key": map[string]any{"nested": "value"},
			},
		},
		{
			name: "src scalar overwrites dest map",
			dest: map[string]any{
				"key": map[string]any{"nested": "value"},
			},
			src: map[string]any{
				"key": "scalar",
			},
			wantDest: map[string]any{
				"key": "scalar",
			},
		},
	}

	for index := range tests {
		testCase := tests[index]

		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			helm.MergeMapsInto(testCase.dest, testCase.src)

			assert.Equal(t, testCase.wantDest, testCase.dest)
		})
	}
}

// copyMap creates a shallow copy of a map to avoid test mutation.
func copyMap(m map[string]any) map[string]any {
	result := make(map[string]any, len(m))
	maps.Copy(result, m)

	return result
}
