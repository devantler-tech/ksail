package workload_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	v1alpha1 "github.com/devantler-tech/ksail/v7/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/workload"
	dockerprovider "github.com/devantler-tech/ksail/v7/pkg/svc/provider/docker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ===========================================================================
// parseInteger — string to int64 conversion
// ===========================================================================

func TestParseInteger(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		trimmed    string
		defaultVal string
		want       any
	}{
		{"valid positive integer", "42", "default", int64(42)},
		{"valid negative integer", "-7", "default", int64(-7)},
		{"zero", "0", "default", int64(0)},
		{"non-numeric falls back", "abc", "fallback", "fallback"},
		{"float string parses integer part", "3.14", "fallback", int64(3)},
		{"empty string falls back", "", "fallback", "fallback"},
		{"large number", "999999999", "default", int64(999999999)},
		{"whitespace falls back", " ", "fallback", "fallback"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := workload.ExportParseInteger(testCase.trimmed, testCase.defaultVal)
			assert.Equal(t, testCase.want, got)
		})
	}
}

// ===========================================================================
// parseNumber — string to float64 conversion
// ===========================================================================

func TestParseNumber(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		trimmed    string
		defaultVal string
		want       any
	}{
		{"valid float", "3.14", "default", float64(3.14)},
		{"integer as float", "42", "default", float64(42)},
		{"negative float", "-1.5", "default", float64(-1.5)},
		{"zero", "0", "default", float64(0)},
		{"non-numeric falls back", "abc", "fallback", "fallback"},
		{"empty string falls back", "", "fallback", "fallback"},
		{"scientific notation", "1e3", "default", float64(1000)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := workload.ExportParseNumber(tt.trimmed, tt.defaultVal)
			assert.Equal(t, tt.want, got)
		})
	}
}

// ===========================================================================
// parseBoolean — string to bool conversion
// ===========================================================================

func TestParseBoolean(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		trimmed    string
		defaultVal string
		want       any
	}{
		{"true string", "true", "default", true},
		{"false string", "false", "default", false},
		{"True capitalized falls back", "True", "fallback", "fallback"},
		{"FALSE uppercase falls back", "FALSE", "fallback", "fallback"},
		{"yes falls back", "yes", "fallback", "fallback"},
		{"empty string falls back", "", "fallback", "fallback"},
		{"1 falls back", "1", "fallback", "fallback"},
		{"0 falls back", "0", "fallback", "fallback"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := workload.ExportParseBoolean(tt.trimmed, tt.defaultVal)
			assert.Equal(t, tt.want, got)
		})
	}
}

// ===========================================================================
// inferYAMLType — YAML-native type inference
// ===========================================================================

func TestInferYAMLType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		trimmed    string
		defaultVal string
		wantType   string // "int", "float", "bool", "string", "default"
	}{
		{"integer value", "42", "default", "float"},
		{"boolean true", "true", "default", "bool"},
		{"boolean false", "false", "default", "bool"},
		{"float value", "3.14", "default", "float"},
		{"string value", "hello", "default", "string"},
		{"null returns default", "null", "default", "default"},
		{"empty returns default", "", "default", "default"},
		{"tilde returns default", "~", "default", "default"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := workload.ExportInferYAMLType(testCase.trimmed, testCase.defaultVal)

			switch testCase.wantType {
			case "int":
				assert.IsType(t, 0, got)
			case "float":
				assert.IsType(t, float64(0), got)
			case "bool":
				assert.IsType(t, false, got)
			case "string":
				assert.IsType(t, "", got)
			case "default":
				assert.Equal(t, testCase.defaultVal, got)
			}
		})
	}
}

// ===========================================================================
// schemaNodeType — JSON schema type extraction
// ===========================================================================

func TestSchemaNodeType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		schema map[string]any
		want   string
	}{
		{"string type", map[string]any{"type": "string"}, "string"},
		{"integer type", map[string]any{"type": "integer"}, "integer"},
		{"boolean type", map[string]any{"type": "boolean"}, "boolean"},
		{"number type", map[string]any{"type": "number"}, "number"},
		{"object type", map[string]any{"type": "object"}, "object"},
		{"array type", map[string]any{"type": "array"}, "array"},
		{
			"type array picks first non-null",
			map[string]any{"type": []any{"null", "string"}},
			"string",
		},
		{
			"type array with integer first",
			map[string]any{"type": []any{"integer", "null"}},
			"integer",
		},
		{
			"type array with only null",
			map[string]any{"type": []any{"null"}},
			"",
		},
		{"no type field", map[string]any{"description": "test"}, ""},
		{"empty schema", map[string]any{}, ""},
		{"nil schema", nil, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := workload.ExportSchemaNodeType(tt.schema)
			assert.Equal(t, tt.want, got)
		})
	}
}

// ===========================================================================
// isNumericIndex — digit-only string check
// ===========================================================================

func TestIsNumericIndex(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"empty string", "", false},
		{"single digit", "0", true},
		{"multiple digits", "123", true},
		{"has letter", "12a", false},
		{"leading zeros", "007", true},
		{"negative", "-1", false},
		{"decimal", "1.5", false},
		{"space", " ", false},
		{"large number", "999999999", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := workload.ExportIsNumericIndex(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

// ===========================================================================
// parseJSONSchema — JSON bytes to map conversion
// ===========================================================================

func TestParseJSONSchema(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		data    []byte
		wantNil bool
	}{
		{
			"valid JSON schema",
			[]byte(`{"type":"object","properties":{"name":{"type":"string"}}}`),
			false,
		},
		{"empty JSON object", []byte(`{}`), false},
		{"invalid JSON", []byte(`not json`), true},
		{"empty bytes", []byte{}, true},
		{"null JSON", []byte(`null`), true},
		{"JSON array is nil", []byte(`[]`), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := workload.ExportParseJSONSchema(tt.data)
			if tt.wantNil {
				assert.Nil(t, got)
			} else {
				assert.NotNil(t, got)
			}
		})
	}
}

//nolint:varnamelen // Short names keep the table-driven tests readable.
func TestParseJSONSchema_ContentPreserved(t *testing.T) {
	t.Parallel()

	data := []byte(
		`{"type":"object","properties":{"replicas":{"type":"integer"},"name":{"type":"string"}}}`,
	)
	schema := workload.ExportParseJSONSchema(data)
	require.NotNil(t, schema)

	assert.Equal(t, "object", schema["type"])

	props, ok := schema["properties"].(map[string]any)
	require.True(t, ok)
	assert.Len(t, props, 2)

	replicas, ok := props["replicas"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "integer", replicas["type"])

	name, ok := props["name"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "string", name["type"])
}

// ===========================================================================
// resolveFromProperties — schema property resolution
// ===========================================================================

func TestResolveFromProperties(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		schema  map[string]any
		key     string
		wantNil bool
	}{
		{
			"existing property",
			map[string]any{
				"properties": map[string]any{
					"name": map[string]any{"type": "string"},
				},
			},
			"name",
			false,
		},
		{
			"non-existing property",
			map[string]any{
				"properties": map[string]any{
					"name": map[string]any{"type": "string"},
				},
			},
			"age",
			true,
		},
		{"no properties field", map[string]any{"type": "object"}, "name", true},
		{"empty schema", map[string]any{}, "name", true},
		{
			"property is not a map",
			map[string]any{
				"properties": map[string]any{
					"name": "not a map",
				},
			},
			"name",
			true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := workload.ExportResolveFromProperties(tt.schema, tt.key)
			if tt.wantNil {
				assert.Nil(t, got)
			} else {
				assert.NotNil(t, got)
			}
		})
	}
}

// ===========================================================================
// resolveFromItems — array schema item resolution
// ===========================================================================

func TestResolveFromItems(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		schema  map[string]any
		key     string
		wantNil bool
	}{
		{
			"numeric index returns items schema",
			map[string]any{"items": map[string]any{"type": "string"}},
			"0",
			false,
		},
		{
			"non-numeric key returns nil",
			map[string]any{"items": map[string]any{"type": "string"}},
			"name",
			true,
		},
		{
			"no items field",
			map[string]any{"type": "array"},
			"0",
			true,
		},
		{
			"large numeric index",
			map[string]any{"items": map[string]any{"type": "object"}},
			"999",
			false,
		},
		{
			"items is not a map",
			map[string]any{"items": "not a map"},
			"0",
			true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := workload.ExportResolveFromItems(tt.schema, tt.key)
			if tt.wantNil {
				assert.Nil(t, got)
			} else {
				assert.NotNil(t, got)
			}
		})
	}
}

// ===========================================================================
// resolveFromCombiners — allOf/anyOf/oneOf resolution
// ===========================================================================

//nolint:funlen // Table-driven test coverage is naturally long.
func TestResolveFromCombiners(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		schema  map[string]any
		key     string
		wantNil bool
	}{
		{
			"allOf with matching property",
			map[string]any{
				"allOf": []any{
					map[string]any{
						"properties": map[string]any{
							"name": map[string]any{"type": "string"},
						},
					},
				},
			},
			"name",
			false,
		},
		{
			"anyOf with matching property",
			map[string]any{
				"anyOf": []any{
					map[string]any{
						"properties": map[string]any{
							"age": map[string]any{"type": "integer"},
						},
					},
				},
			},
			"age",
			false,
		},
		{
			"oneOf with matching property",
			map[string]any{
				"oneOf": []any{
					map[string]any{
						"properties": map[string]any{
							"count": map[string]any{"type": "number"},
						},
					},
				},
			},
			"count",
			false,
		},
		{
			"no combiner",
			map[string]any{"type": "object"},
			"name",
			true,
		},
		{
			"combiner without matching key",
			map[string]any{
				"allOf": []any{
					map[string]any{
						"properties": map[string]any{
							"name": map[string]any{"type": "string"},
						},
					},
				},
			},
			"nonexistent",
			true,
		},
		{
			"combiner entry is not a map",
			map[string]any{
				"allOf": []any{"not a map"},
			},
			"name",
			true,
		},
		{
			"combiner array is empty",
			map[string]any{
				"allOf": []any{},
			},
			"name",
			true,
		},
		{
			"allOf fallback to anyOf",
			map[string]any{
				"allOf": []any{
					map[string]any{
						"properties": map[string]any{
							"x": map[string]any{"type": "string"},
						},
					},
				},
				"anyOf": []any{
					map[string]any{
						"properties": map[string]any{
							"y": map[string]any{"type": "integer"},
						},
					},
				},
			},
			"y",
			false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := workload.ExportResolveFromCombiners(tt.schema, tt.key)
			if tt.wantNil {
				assert.Nil(t, got)
			} else {
				assert.NotNil(t, got)
			}
		})
	}
}

// ===========================================================================
// schemaCacheDir — cache directory location
// ===========================================================================

func TestSchemaCacheDir(t *testing.T) {
	t.Parallel()

	dir := workload.ExportSchemaCacheDir()
	assert.NotEmpty(t, dir)
	assert.Contains(t, dir, "ksail")
	assert.Contains(t, dir, "kubeconform")
}

// ===========================================================================
// schemaCacheFileName — URL to safe filename
// ===========================================================================

func TestSchemaCacheFileName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		url  string
	}{
		{"simple URL", "https://example.com/schema.json"},
		{"URL with path", "https://raw.githubusercontent.com/org/repo/main/schema.json"},
		{"short URL", "http://a.com/s"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := workload.ExportSchemaCacheFileName(tt.url)
			assert.NotEmpty(t, got)
			assert.True(t, strings.HasSuffix(got, ".json"), "expected .json suffix, got %q", got)
			assert.NotContains(t, got, "://")
			assert.NotContains(t, got, "/")
		})
	}
}

func TestSchemaCacheFileName_MaxLength(t *testing.T) {
	t.Parallel()

	longURL := "https://example.com/" + strings.Repeat("abcdefgh", 100) + "/schema.json"
	got := workload.ExportSchemaCacheFileName(longURL)
	assert.LessOrEqual(t, len(got), 200)
	assert.True(t, strings.HasSuffix(got, ".json"))
}

func TestSchemaCacheFileName_Deterministic(t *testing.T) {
	t.Parallel()

	url := "https://example.com/schema.json"
	a := workload.ExportSchemaCacheFileName(url)
	b := workload.ExportSchemaCacheFileName(url)
	assert.Equal(t, a, b, "same URL should produce same filename")
}

// ===========================================================================
// distributionToLabelScheme — distribution enum mapping
// ===========================================================================

func TestDistributionToLabelScheme(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		distribution v1alpha1.Distribution
		want         dockerprovider.LabelScheme
	}{
		{"vanilla → kind", v1alpha1.DistributionVanilla, dockerprovider.LabelSchemeKind},
		{"k3s → k3d", v1alpha1.DistributionK3s, dockerprovider.LabelSchemeK3d},
		{"talos → talos", v1alpha1.DistributionTalos, dockerprovider.LabelSchemeTalos},
		{"vcluster → vcluster", v1alpha1.DistributionVCluster, dockerprovider.LabelSchemeVCluster},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := workload.ExportDistributionToLabelScheme(tt.distribution)
			assert.Equal(t, tt.want, got)
		})
	}
}

// ===========================================================================
// enqueueIfCurrent — stale generation skip
// ===========================================================================

func TestEnqueueIfCurrent_SkipsStaleGeneration(t *testing.T) {
	t.Parallel()

	state := workload.ExportNewDebounceState()
	workload.ExportSetDebounceState(state, 10, "file.yaml")

	applyCh := make(chan string, 1)
	workload.ExportEnqueueIfCurrent(state, 5, applyCh)

	select {
	case <-applyCh:
		t.Fatal("should not have enqueued for stale generation")
	case <-time.After(100 * time.Millisecond):
		// expected - no message
	}
}

func TestEnqueueIfCurrent_MatchingGeneration(t *testing.T) {
	t.Parallel()

	state := workload.ExportNewDebounceState()
	workload.ExportSetDebounceState(state, 7, "test.yaml")

	applyCh := make(chan string, 1)
	workload.ExportEnqueueIfCurrent(state, 7, applyCh)

	select {
	case file := <-applyCh:
		assert.Equal(t, "test.yaml", file)
	case <-time.After(500 * time.Millisecond):
		t.Fatal("should have enqueued for current generation")
	}
}

// ===========================================================================
// debounce state — lifecycle tests
// ===========================================================================

func TestDebounceState_InitialValues(t *testing.T) {
	t.Parallel()

	state := workload.ExportNewDebounceState()
	require.NotNil(t, state)
	assert.Equal(t, uint64(0), workload.ExportGetGeneration(state))
	assert.Empty(t, workload.ExportGetLastFile(state))
}

func TestDebounceState_SetAndGet(t *testing.T) {
	t.Parallel()

	state := workload.ExportNewDebounceState()
	workload.ExportSetDebounceState(state, 42, "test.yaml")
	assert.Equal(t, uint64(42), workload.ExportGetGeneration(state))
	assert.Equal(t, "test.yaml", workload.ExportGetLastFile(state))
}

func TestDebounceState_CancelIsSafe(t *testing.T) {
	t.Parallel()

	state := workload.ExportNewDebounceState()
	// Cancel on fresh state should not panic
	workload.ExportCancelPendingDebounce(state)
}

func TestDebounceState_CancelAfterSchedule(t *testing.T) {
	t.Parallel()

	state := workload.ExportNewDebounceState()
	applyCh := make(chan string, 1)

	workload.ExportScheduleApply(state, "file.yaml", applyCh)
	workload.ExportCancelPendingDebounce(state)

	// After canceling, the debounce should not fire
	select {
	case <-applyCh:
		t.Fatal("should not have received message after cancel")
	case <-time.After(workload.ExportDebounceInterval + 200*time.Millisecond):
		// expected
	}
}

// ===========================================================================
// detectChangedFile — additional edge cases
// ===========================================================================

func TestDetectChangedFile_DeletedFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	testFile := filepath.Join(dir, "ephemeral.yaml")
	require.NoError(t, os.WriteFile(testFile, []byte("data"), 0o600))

	snapshot := workload.ExportBuildFileSnapshot(dir)
	require.NotEmpty(t, snapshot)

	// Delete the file
	require.NoError(t, os.Remove(testFile))

	changed := workload.ExportDetectChangedFile(dir, snapshot)
	assert.NotEmpty(t, changed, "should detect deleted file")
}

func TestDetectChangedFile_NewFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "existing.yaml"), []byte("a"), 0o600))

	snapshot := workload.ExportBuildFileSnapshot(dir)

	// Add a new file
	newFile := filepath.Join(dir, "new.yaml")
	require.NoError(t, os.WriteFile(newFile, []byte("b"), 0o600))

	changed := workload.ExportDetectChangedFile(dir, snapshot)
	assert.NotEmpty(t, changed, "should detect new file")
}

// ===========================================================================
// getSchemaTypeAtPath — comprehensive schema traversal
// ===========================================================================

func TestGetSchemaTypeAtPath_NestedSchema(t *testing.T) {
	t.Parallel()

	schemaJSON := `{
		"type": "object",
		"properties": {
			"spec": {
				"type": "object",
				"properties": {
					"replicas": {"type": "integer"},
					"template": {
						"type": "object",
						"properties": {
							"containers": {
								"type": "array",
								"items": {
									"type": "object",
									"properties": {
										"name": {"type": "string"},
										"image": {"type": "string"}
									}
								}
							}
						}
					}
				}
			}
		}
	}`

	var schema map[string]any
	require.NoError(t, json.Unmarshal([]byte(schemaJSON), &schema))

	tests := []struct {
		name string
		path string
		want string
	}{
		{"top-level spec", "spec", "object"},
		{"nested replicas", "spec/replicas", "integer"},
		{"nested template", "spec/template", "object"},
		{"array type", "spec/template/containers", "array"},
		{"nonexistent deep path", "spec/nonexistent/deep", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := workload.ExportGetSchemaTypeAtPath(schema, tt.path)
			assert.Equal(t, tt.want, got)
		})
	}
}

// ===========================================================================
// Exported constants
// ===========================================================================

func TestExportedTimeConstants(t *testing.T) {
	t.Parallel()

	assert.Greater(t, workload.ExportDebounceInterval, time.Duration(0),
		"debounce interval should be positive")
	assert.Greater(t, workload.ExportPollInterval, time.Duration(0),
		"poll interval should be positive")
}

// ===========================================================================
// findKustomizationDir — additional edge cases
// ===========================================================================

func TestFindKustomizationDir_NonexistentFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	result := workload.ExportFindKustomizationDir("/nonexistent/file.yaml", dir)
	assert.Equal(t, dir, result, "should fall back to root when file doesn't exist")
}

func TestFindKustomizationDir_FileInRoot(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "kustomization.yaml"), []byte("---"), 0o600))
	testFile := filepath.Join(dir, "test.yaml")
	require.NoError(t, os.WriteFile(testFile, []byte("data"), 0o600))

	result := workload.ExportFindKustomizationDir(testFile, dir)
	assert.Equal(t, dir, result)
}
