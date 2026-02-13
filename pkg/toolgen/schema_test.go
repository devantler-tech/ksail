package toolgen_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/toolgen"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockEnumValue implements the enumValuer interface for testing.
type mockEnumValue struct {
	value       string
	validValues []string
}

func (m *mockEnumValue) Set(s string) error {
	m.value = s

	return nil
}

func (m *mockEnumValue) String() string {
	return m.value
}

func (m *mockEnumValue) Type() string {
	return "mockEnum"
}

func (m *mockEnumValue) ValidValues() []string {
	return m.validValues
}

// mockDefaultEnumValue implements both enumValuer and defaulter interfaces.
type mockDefaultEnumValue struct {
	mockEnumValue

	defaultVal string
}

func (m *mockDefaultEnumValue) Default() any {
	return m.defaultVal
}

// newTestCmd creates a minimal cobra.Command suitable for toolgen tests.
func newTestCmd() *cobra.Command {
	return &cobra.Command{
		Use:  "test",
		RunE: func(_ *cobra.Command, _ []string) error { return nil },
	}
}

// generateToolProperties generates tools and returns the first tool's properties.
func generateToolProperties(t *testing.T, cmd *cobra.Command) map[string]any {
	t.Helper()

	tools := toolgen.GenerateTools(cmd, toolgen.DefaultOptions())

	require.NotEmpty(t, tools, "expected at least one tool")

	properties, propertiesOK := tools[0].Parameters["properties"].(map[string]any)

	require.True(t, propertiesOK, "expected properties in schema")

	return properties
}

// assertPropertyField checks a single field value inside a property map.
func assertPropertyField(
	t *testing.T,
	propMap map[string]any,
	field string,
	expected any,
) {
	t.Helper()

	actual, exists := propMap[field]

	if !exists {
		t.Errorf("expected field %q, got none", field)

		return
	}

	assert.Equal(t, expected, actual, "field %q mismatch", field)
}

type standardTypeTest struct {
	name     string
	setup    func(*cobra.Command)
	flagName string
	expected map[string]any
}

func standardTypeTestCases() []standardTypeTest {
	return []standardTypeTest{
		{
			name:     "boolean flag",
			setup:    func(cmd *cobra.Command) { cmd.Flags().Bool("verbose", false, "Enable verbose output") },
			flagName: "verbose",
			expected: map[string]any{"type": "boolean", "description": "Enable verbose output"},
		},
		{
			name:     "integer flag",
			setup:    func(cmd *cobra.Command) { cmd.Flags().Int("count", 5, "Number of items") },
			flagName: "count",
			expected: map[string]any{
				"type":        "integer",
				"description": "Number of items",
				"default":     int64(5),
			},
		},
		{
			name:     "float flag",
			setup:    func(cmd *cobra.Command) { cmd.Flags().Float64("ratio", 0.5, "Ratio value") },
			flagName: "ratio",
			expected: map[string]any{
				"type":        "number",
				"description": "Ratio value",
				"default":     float64(0.5),
			},
		},
		{
			name:     "string flag",
			setup:    func(cmd *cobra.Command) { cmd.Flags().String("name", "default", "Name value") },
			flagName: "name",
			expected: map[string]any{
				"type":        "string",
				"description": "Name value",
				"default":     "default",
			},
		},
		{
			name:     "string slice flag",
			setup:    func(cmd *cobra.Command) { cmd.Flags().StringSlice("tags", []string{}, "Tag values") },
			flagName: "tags",
			expected: map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Tag values",
			},
		},
		{
			name:     "int slice flag",
			setup:    func(cmd *cobra.Command) { cmd.Flags().IntSlice("ports", []int{}, "Port numbers") },
			flagName: "ports",
			expected: map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "integer"},
				"description": "Port numbers",
			},
		},
	}
}

// Test buildStandardProperty via buildParameterSchema.
func TestBuildParameterSchema_StandardTypes(t *testing.T) {
	t.Parallel()

	for _, testCase := range standardTypeTestCases() {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			cmd := newTestCmd()
			testCase.setup(cmd)

			properties := generateToolProperties(t, cmd)

			propMap, propOK := properties[testCase.flagName].(map[string]any)

			require.True(t, propOK, "property %q should be a map", testCase.flagName)

			for field, expected := range testCase.expected {
				assertPropertyField(t, propMap, field, expected)
			}
		})
	}

	t.Run("duration flag", func(t *testing.T) {
		t.Parallel()

		cmd := newTestCmd()
		cmd.Flags().Duration("timeout", 0, "Timeout duration")

		properties := generateToolProperties(t, cmd)

		propMap, propOK := properties["timeout"].(map[string]any)
		require.True(t, propOK, "property %q should be a map", "timeout")

		assertPropertyField(t, propMap, "type", "string")
		assertPropertyField(t, propMap, "description", "Timeout duration (format: 1h30m, 5m, 30s)")
	})
}

type defaultValueTest struct {
	name         string
	setup        func(*cobra.Command)
	flagName     string
	expectedType string
	expectedDef  any
}

func defaultValueTestCases() []defaultValueTest {
	return []defaultValueTest{
		{
			name:         "boolean true default",
			setup:        func(cmd *cobra.Command) { cmd.Flags().Bool("enable", true, "Enable feature") },
			flagName:     "enable",
			expectedType: "boolean",
			expectedDef:  true,
		},
		{
			name:         "boolean false default omitted",
			setup:        func(cmd *cobra.Command) { cmd.Flags().Bool("disable", false, "Disable feature") },
			flagName:     "disable",
			expectedType: "boolean",
			expectedDef:  nil,
		},
		{
			name:         "integer default",
			setup:        func(cmd *cobra.Command) { cmd.Flags().Int("workers", 42, "Number of workers") },
			flagName:     "workers",
			expectedType: "integer",
			expectedDef:  int64(42),
		},
		{
			name: "float default",
			setup: func(cmd *cobra.Command) {
				cmd.Flags().Float64("threshold", 3.14, "Threshold value")
			},
			flagName:     "threshold",
			expectedType: "number",
			expectedDef:  float64(3.14),
		},
		{
			name:         "string default",
			setup:        func(cmd *cobra.Command) { cmd.Flags().String("output", "json", "Output format") },
			flagName:     "output",
			expectedType: "string",
			expectedDef:  "json",
		},
	}
}

// Test convertDefaultValue function behavior.
func TestBuildParameterSchema_DefaultValueConversion(t *testing.T) {
	t.Parallel()

	for _, testCase := range defaultValueTestCases() {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			cmd := newTestCmd()
			testCase.setup(cmd)

			properties := generateToolProperties(t, cmd)

			propMap, propOK := properties[testCase.flagName].(map[string]any)

			require.True(t, propOK, "property %q should be a map", testCase.flagName)

			assert.Equal(t, testCase.expectedType, propMap["type"], "type mismatch")
			assert.Equal(t, testCase.expectedDef, propMap["default"], "default mismatch")
		})
	}
}

type requiredFieldTest struct {
	name     string
	setup    func(*cobra.Command)
	expected []string
}

func requiredFieldTestCases() []requiredFieldTest {
	return []requiredFieldTest{
		{
			name: "no required fields",
			setup: func(cmd *cobra.Command) {
				cmd.Flags().String("name", "default", "Name")
				cmd.Flags().Int("count", 5, "Count")
				cmd.Flags().Bool("verbose", false, "Verbose")
			},
			expected: []string{},
		},
		{
			name: "string without default is required",
			setup: func(cmd *cobra.Command) {
				cmd.Flags().String("name", "", "Name")
				cmd.Flags().String("output", "json", "Output format")
			},
			expected: []string{"name"},
		},
		{
			name: "empty string port is required",
			setup: func(cmd *cobra.Command) {
				cmd.Flags().String("port", "", "Port number")
				cmd.Flags().String("host", "localhost", "Host")
			},
			expected: []string{"port"},
		},
		{
			name: "bool is never required",
			setup: func(cmd *cobra.Command) {
				cmd.Flags().Bool("verbose", false, "Verbose")
				cmd.Flags().Bool("quiet", true, "Quiet")
			},
			expected: []string{},
		},
	}
}

// Test required field detection.
func TestBuildParameterSchema_RequiredFields(t *testing.T) {
	t.Parallel()

	for _, testCase := range requiredFieldTestCases() {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			cmd := newTestCmd()
			testCase.setup(cmd)

			tools := toolgen.GenerateTools(cmd, toolgen.DefaultOptions())

			require.NotEmpty(t, tools)

			var actualRequired []string
			if req, reqOK := tools[0].Parameters["required"].([]string); reqOK {
				actualRequired = req
			}

			assert.Len(t, actualRequired, len(testCase.expected),
				"required count mismatch: got %v", actualRequired)

			for _, field := range testCase.expected {
				assert.Contains(t, actualRequired, field)
			}
		})
	}
}

// Test positional arguments handling.
func TestBuildParameterSchema_PositionalArgs(t *testing.T) {
	t.Parallel()

	t.Run("command with positional args", func(t *testing.T) {
		t.Parallel()

		cmd := &cobra.Command{
			Use:  "test",
			Args: cobra.MinimumNArgs(1),
			RunE: func(_ *cobra.Command, _ []string) error { return nil },
		}

		properties := generateToolProperties(t, cmd)

		argsProp, hasArgs := properties["args"].(map[string]any)

		require.True(t, hasArgs, "expected 'args' property")
		assert.Equal(t, "array", argsProp["type"])

		items, itemsOK := argsProp["items"].(map[string]any)

		require.True(t, itemsOK, "expected items map")
		assert.Equal(t, "string", items["type"])
		assert.Equal(t,
			"Positional arguments for the command",
			argsProp["description"],
		)
	})

	t.Run("command without args validator", func(t *testing.T) {
		t.Parallel()

		cmd := newTestCmd()
		properties := generateToolProperties(t, cmd)

		_, hasArgs := properties["args"]
		assert.False(t, hasArgs, "unexpected 'args' property found")
	})
}

// Test help flag exclusion.
func TestBuildParameterSchema_ExcludesHelpFlag(t *testing.T) {
	t.Parallel()

	cmd := newTestCmd()
	cmd.Flags().String("name", "", "Name value")
	cmd.Flags().Bool("help", false, "Show help")

	properties := generateToolProperties(t, cmd)

	_, hasHelp := properties["help"]
	assert.False(t, hasHelp, "help flag should be excluded from schema")

	_, hasName := properties["name"]
	assert.True(t, hasName, "expected name property to be present")
}

// Test buildEnumProperty via enum-valued flags.
func TestBuildParameterSchema_EnumValues(t *testing.T) {
	t.Parallel()

	cmd := newTestCmd()

	enumFlag := &mockEnumValue{
		value:       "option1",
		validValues: []string{"option1", "option2", "option3"},
	}
	cmd.Flags().Var(enumFlag, "format", "Output format")

	properties := generateToolProperties(t, cmd)

	propMap, propOK := properties["format"].(map[string]any)

	require.True(t, propOK, "expected 'format' property")

	assert.Equal(t, "string", propMap["type"])

	enumSlice, enumOK := propMap["enum"].([]string)

	require.True(t, enumOK, "expected enum to be []string")
	assert.Equal(t, []string{"option1", "option2", "option3"}, enumSlice)

	desc, descOK := propMap["description"].(string)

	require.True(t, descOK, "expected description to be string")
	assert.Contains(t, desc, "option1", "description should mention option1")
	assert.Contains(t, desc, "option2", "description should mention option2")
}

// Test buildEnumProperty with default value.
func TestBuildParameterSchema_EnumWithDefault(t *testing.T) {
	t.Parallel()

	cmd := newTestCmd()

	enumFlag := &mockDefaultEnumValue{
		mockEnumValue: mockEnumValue{
			value:       "json",
			validValues: []string{"json", "yaml", "xml"},
		},
		defaultVal: "json",
	}
	cmd.Flags().Var(enumFlag, "output", "Output format")

	properties := generateToolProperties(t, cmd)

	propMap, propOK := properties["output"].(map[string]any)

	require.True(t, propOK, "expected 'output' property")

	assert.Equal(t, "json", propMap["default"])
}

// Test buildEnumProperty with empty valid values.
func TestBuildParameterSchema_EnumEmptyValues(t *testing.T) {
	t.Parallel()

	cmd := newTestCmd()

	enumFlag := &mockEnumValue{
		value:       "test",
		validValues: []string{},
	}
	cmd.Flags().Var(enumFlag, "mode", "Mode setting")

	properties := generateToolProperties(t, cmd)

	propMap, propOK := properties["mode"].(map[string]any)

	require.True(t, propOK, "expected 'mode' property")

	_, hasEnum := propMap["enum"]
	assert.False(t, hasEnum, "empty enum should not generate enum field")
}

// Test mapFlagTypeToJSONType via schema generation.
func TestMapFlagTypes(t *testing.T) {
	t.Parallel()

	flagTests := []struct {
		flagType     string
		setupFlag    func(*pflag.FlagSet)
		expectedType string
	}{
		{"bool", func(fs *pflag.FlagSet) { fs.Bool("test", false, "test") }, "boolean"},
		{"int", func(fs *pflag.FlagSet) { fs.Int("test", 0, "test") }, "integer"},
		{"int64", func(fs *pflag.FlagSet) { fs.Int64("test", 0, "test") }, "integer"},
		{"uint", func(fs *pflag.FlagSet) { fs.Uint("test", 0, "test") }, "integer"},
		{"float64", func(fs *pflag.FlagSet) { fs.Float64("test", 0, "test") }, "number"},
		{"string", func(fs *pflag.FlagSet) { fs.String("test", "", "test") }, "string"},
		{
			"stringSlice",
			func(fs *pflag.FlagSet) { fs.StringSlice("test", nil, "test") },
			"array",
		},
	}

	for _, testCase := range flagTests {
		t.Run(testCase.flagType, func(t *testing.T) {
			t.Parallel()

			cmd := newTestCmd()
			testCase.setupFlag(cmd.Flags())

			properties := generateToolProperties(t, cmd)

			propMap, propOK := properties["test"].(map[string]any)

			require.True(t, propOK, "expected 'test' property")
			assert.Equal(t, testCase.expectedType, propMap["type"])
		})
	}
}
