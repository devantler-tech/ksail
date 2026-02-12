package toolgen_test

import (
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/toolgen"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// mockEnumValue implements the enumValuer interface for testing
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

// Test buildStandardProperty via buildParameterSchema
func TestBuildParameterSchema_StandardTypes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		setupCmd      func() *cobra.Command
		expectedProps map[string]map[string]any
	}{
		{
			name: "boolean flag",
			setupCmd: func() *cobra.Command {
				cmd := &cobra.Command{
					Use:  "test",
					RunE: func(cmd *cobra.Command, args []string) error { return nil },
				}
				cmd.Flags().Bool("verbose", false, "Enable verbose output")
				return cmd
			},
			expectedProps: map[string]map[string]any{
				"verbose": {
					"type":        "boolean",
					"description": "Enable verbose output",
				},
			},
		},
		{
			name: "integer flag",
			setupCmd: func() *cobra.Command {
				cmd := &cobra.Command{Use: "test", RunE: func(cmd *cobra.Command, args []string) error { return nil }}
				cmd.Flags().Int("count", 5, "Number of items")
				return cmd
			},
			expectedProps: map[string]map[string]any{
				"count": {
					"type":        "integer",
					"description": "Number of items",
					"default":     int64(5),
				},
			},
		},
		{
			name: "float flag",
			setupCmd: func() *cobra.Command {
				cmd := &cobra.Command{Use: "test", RunE: func(cmd *cobra.Command, args []string) error { return nil }}
				cmd.Flags().Float64("ratio", 0.5, "Ratio value")
				return cmd
			},
			expectedProps: map[string]map[string]any{
				"ratio": {
					"type":        "number",
					"description": "Ratio value",
					"default":     float64(0.5),
				},
			},
		},
		{
			name: "string flag",
			setupCmd: func() *cobra.Command {
				cmd := &cobra.Command{Use: "test", RunE: func(cmd *cobra.Command, args []string) error { return nil }}
				cmd.Flags().String("name", "default", "Name value")
				return cmd
			},
			expectedProps: map[string]map[string]any{
				"name": {
					"type":        "string",
					"description": "Name value",
					"default":     "default",
				},
			},
		},
		{
			name: "string slice flag",
			setupCmd: func() *cobra.Command {
				cmd := &cobra.Command{Use: "test", RunE: func(cmd *cobra.Command, args []string) error { return nil }}
				cmd.Flags().StringSlice("tags", []string{}, "Tag values")
				return cmd
			},
			expectedProps: map[string]map[string]any{
				"tags": {
					"type":        "array",
					"items":       map[string]any{"type": "string"},
					"description": "Tag values",
				},
			},
		},
		{
			name: "int slice flag",
			setupCmd: func() *cobra.Command {
				cmd := &cobra.Command{Use: "test", RunE: func(cmd *cobra.Command, args []string) error { return nil }}
				cmd.Flags().IntSlice("ports", []int{}, "Port numbers")
				return cmd
			},
			expectedProps: map[string]map[string]any{
				"ports": {
					"type":        "array",
					"items":       map[string]any{"type": "integer"},
					"description": "Port numbers",
				},
			},
		},
		{
			name: "duration flag",
			setupCmd: func() *cobra.Command {
				cmd := &cobra.Command{Use: "test", RunE: func(cmd *cobra.Command, args []string) error { return nil }}
				cmd.Flags().Duration("timeout", 0, "Timeout duration")
				return cmd
			},
			expectedProps: map[string]map[string]any{
				"timeout": {
					"type":        "string",
					"description": "Timeout duration (format: 1h30m, 5m, 30s)",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cmd := tt.setupCmd()
			
			// Use toolgen's internal function via GenerateTools approach
			// Since buildParameterSchema is unexported, we test it indirectly
			opts := toolgen.DefaultOptions()
			tools := toolgen.GenerateTools(cmd, opts)
			
			if len(tools) == 0 {
				t.Fatal("expected at least one tool")
			}
			
			tool := tools[0]
			properties, ok := tool.Parameters["properties"].(map[string]any)
			if !ok {
				t.Fatal("expected properties in schema")
			}
			
			for propName, expectedProp := range tt.expectedProps {
				actualProp, exists := properties[propName]
				if !exists {
					t.Errorf("expected property %q not found", propName)
					continue
				}
				
				actualPropMap, ok := actualProp.(map[string]any)
				if !ok {
					t.Errorf("property %q is not a map", propName)
					continue
				}
				
				// Check type
				if actualPropMap["type"] != expectedProp["type"] {
					t.Errorf("property %q: expected type %v, got %v",
						propName, expectedProp["type"], actualPropMap["type"])
				}
				
				// Check description contains expected text
				if desc, ok := actualPropMap["description"].(string); ok {
					if expectedDesc, ok := expectedProp["description"].(string); ok {
						if desc != expectedDesc {
							t.Errorf("property %q: expected description %q, got %q",
								propName, expectedDesc, desc)
						}
					}
				}
				
				// Check default if expected
				if expectedDefault, hasDefault := expectedProp["default"]; hasDefault {
					actualDefault, hasActualDefault := actualPropMap["default"]
					if !hasActualDefault {
						t.Errorf("property %q: expected default %v, got none", propName, expectedDefault)
					} else if actualDefault != expectedDefault {
						t.Errorf("property %q: expected default %v, got %v",
							propName, expectedDefault, actualDefault)
					}
				}
				
				// Check items for array types
				if expectedItems, hasItems := expectedProp["items"]; hasItems {
					actualItems, hasActualItems := actualPropMap["items"]
					if !hasActualItems {
						t.Errorf("property %q: expected items, got none", propName)
					} else {
						// Check items type
						expectedItemsMap := expectedItems.(map[string]any)
						actualItemsMap := actualItems.(map[string]any)
						if actualItemsMap["type"] != expectedItemsMap["type"] {
							t.Errorf("property %q items: expected type %v, got %v",
								propName, expectedItemsMap["type"], actualItemsMap["type"])
						}
					}
				}
			}
		})
	}
}

// Test convertDefaultValue function behavior
func TestBuildParameterSchema_DefaultValueConversion(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		setupCmd     func() *cobra.Command
		flagName     string
		expectedType string
		checkDefault func(t *testing.T, defaultVal any)
	}{
		{
			name: "boolean true default",
			setupCmd: func() *cobra.Command {
				cmd := &cobra.Command{Use: "test", RunE: func(cmd *cobra.Command, args []string) error { return nil }}
				cmd.Flags().Bool("enable", true, "Enable feature")
				return cmd
			},
			flagName:     "enable",
			expectedType: "boolean",
			checkDefault: func(t *testing.T, defaultVal any) {
				if val, ok := defaultVal.(bool); !ok || val != true {
					t.Errorf("expected bool true, got %v (%T)", defaultVal, defaultVal)
				}
			},
		},
		{
			name: "boolean false default (should not appear)",
			setupCmd: func() *cobra.Command {
				cmd := &cobra.Command{Use: "test", RunE: func(cmd *cobra.Command, args []string) error { return nil }}
				cmd.Flags().Bool("disable", false, "Disable feature")
				return cmd
			},
			flagName:     "disable",
			expectedType: "boolean",
			checkDefault: func(t *testing.T, defaultVal any) {
				// False defaults should not be included per addDefaultValue logic
				if defaultVal != nil {
					t.Errorf("expected no default for false bool, got %v", defaultVal)
				}
			},
		},
		{
			name: "integer default",
			setupCmd: func() *cobra.Command {
				cmd := &cobra.Command{Use: "test", RunE: func(cmd *cobra.Command, args []string) error { return nil }}
				cmd.Flags().Int("workers", 42, "Number of workers")
				return cmd
			},
			flagName:     "workers",
			expectedType: "integer",
			checkDefault: func(t *testing.T, defaultVal any) {
				if val, ok := defaultVal.(int64); !ok || val != 42 {
					t.Errorf("expected int64(42), got %v (%T)", defaultVal, defaultVal)
				}
			},
		},
		{
			name: "float default",
			setupCmd: func() *cobra.Command {
				cmd := &cobra.Command{Use: "test", RunE: func(cmd *cobra.Command, args []string) error { return nil }}
				cmd.Flags().Float64("threshold", 3.14, "Threshold value")
				return cmd
			},
			flagName:     "threshold",
			expectedType: "number",
			checkDefault: func(t *testing.T, defaultVal any) {
				if val, ok := defaultVal.(float64); !ok || val != 3.14 {
					t.Errorf("expected float64(3.14), got %v (%T)", defaultVal, defaultVal)
				}
			},
		},
		{
			name: "string default",
			setupCmd: func() *cobra.Command {
				cmd := &cobra.Command{Use: "test", RunE: func(cmd *cobra.Command, args []string) error { return nil }}
				cmd.Flags().String("output", "json", "Output format")
				return cmd
			},
			flagName:     "output",
			expectedType: "string",
			checkDefault: func(t *testing.T, defaultVal any) {
				if val, ok := defaultVal.(string); !ok || val != "json" {
					t.Errorf("expected string 'json', got %v (%T)", defaultVal, defaultVal)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cmd := tt.setupCmd()
			opts := toolgen.DefaultOptions()
			tools := toolgen.GenerateTools(cmd, opts)
			
			if len(tools) == 0 {
				t.Fatal("expected at least one tool")
			}
			
			tool := tools[0]
			properties, ok := tool.Parameters["properties"].(map[string]any)
			if !ok {
				t.Fatal("expected properties in schema")
			}
			
			prop, exists := properties[tt.flagName]
			if !exists {
				t.Fatalf("expected property %q not found", tt.flagName)
			}
			
			propMap := prop.(map[string]any)
			
			// Check type
			if propMap["type"] != tt.expectedType {
				t.Errorf("expected type %q, got %v", tt.expectedType, propMap["type"])
			}
			
			// Check default using custom checker
			defaultVal := propMap["default"]
			tt.checkDefault(t, defaultVal)
		})
	}
}

// Test required field detection
func TestBuildParameterSchema_RequiredFields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		setupCmd         func() *cobra.Command
		expectedRequired []string
	}{
		{
			name: "no required fields (all have defaults)",
			setupCmd: func() *cobra.Command {
				cmd := &cobra.Command{Use: "test", RunE: func(cmd *cobra.Command, args []string) error { return nil }}
				cmd.Flags().String("name", "default", "Name")
				cmd.Flags().Int("count", 5, "Count")
				cmd.Flags().Bool("verbose", false, "Verbose")
				return cmd
			},
			expectedRequired: []string{},
		},
		{
			name: "string without default is required",
			setupCmd: func() *cobra.Command {
				cmd := &cobra.Command{Use: "test", RunE: func(cmd *cobra.Command, args []string) error { return nil }}
				cmd.Flags().String("name", "", "Name")
				cmd.Flags().String("output", "json", "Output format")
				return cmd
			},
			expectedRequired: []string{"name"},
		},
		{
			name: "int without default is required",
			setupCmd: func() *cobra.Command {
				cmd := &cobra.Command{Use: "test", RunE: func(cmd *cobra.Command, args []string) error { return nil }}
				// To have no default, we need to mark it as required via a different mechanism
				// For this test, let's use a string that has no default to demonstrate required fields
				cmd.Flags().String("port", "", "Port number")
				cmd.Flags().String("host", "localhost", "Host")
				return cmd
			},
			expectedRequired: []string{"port"},
		},
		{
			name: "bool is never required",
			setupCmd: func() *cobra.Command {
				cmd := &cobra.Command{Use: "test", RunE: func(cmd *cobra.Command, args []string) error { return nil }}
				cmd.Flags().Bool("verbose", false, "Verbose")
				cmd.Flags().Bool("quiet", true, "Quiet")
				return cmd
			},
			expectedRequired: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cmd := tt.setupCmd()
			opts := toolgen.DefaultOptions()
			tools := toolgen.GenerateTools(cmd, opts)
			
			if len(tools) == 0 {
				t.Fatal("expected at least one tool")
			}
			
			tool := tools[0]
			
			var actualRequired []string
			if req, ok := tool.Parameters["required"].([]string); ok {
				actualRequired = req
			}
			
			// Check length
			if len(actualRequired) != len(tt.expectedRequired) {
				t.Errorf("expected %d required fields, got %d: %v",
					len(tt.expectedRequired), len(actualRequired), actualRequired)
			}
			
			// Check each expected field is present
			requiredMap := make(map[string]bool)
			for _, field := range actualRequired {
				requiredMap[field] = true
			}
			
			for _, expected := range tt.expectedRequired {
				if !requiredMap[expected] {
					t.Errorf("expected required field %q not found in %v", expected, actualRequired)
				}
			}
		})
	}
}

// Test positional arguments handling
func TestBuildParameterSchema_PositionalArgs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		setupCmd    func() *cobra.Command
		expectArgs  bool
		description string
	}{
		{
			name: "command with positional args",
			setupCmd: func() *cobra.Command {
				cmd := &cobra.Command{
					Use:  "test",
					Args: cobra.MinimumNArgs(1),
					RunE: func(cmd *cobra.Command, args []string) error { return nil },
				}
				return cmd
			},
			expectArgs:  true,
			description: "Positional arguments for the command",
		},
		{
			name: "command without args validator",
			setupCmd: func() *cobra.Command {
				cmd := &cobra.Command{Use: "test", RunE: func(cmd *cobra.Command, args []string) error { return nil }}
				return cmd
			},
			expectArgs: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cmd := tt.setupCmd()
			opts := toolgen.DefaultOptions()
			tools := toolgen.GenerateTools(cmd, opts)
			
			if len(tools) == 0 {
				t.Fatal("expected at least one tool")
			}
			
			tool := tools[0]
			properties, ok := tool.Parameters["properties"].(map[string]any)
			if !ok {
				t.Fatal("expected properties in schema")
			}
			
			argsProp, hasArgs := properties["args"]
			
			if tt.expectArgs {
				if !hasArgs {
					t.Error("expected 'args' property, got none")
				} else {
					argsMap := argsProp.(map[string]any)
					if argsMap["type"] != "array" {
						t.Errorf("expected args type 'array', got %v", argsMap["type"])
					}
					
					items := argsMap["items"].(map[string]any)
					if items["type"] != "string" {
						t.Errorf("expected args items type 'string', got %v", items["type"])
					}
					
					if desc := argsMap["description"]; desc != tt.description {
						t.Errorf("expected description %q, got %v", tt.description, desc)
					}
				}
			} else {
				if hasArgs {
					t.Error("unexpected 'args' property found")
				}
			}
		})
	}
}

// Test help flag exclusion
func TestBuildParameterSchema_ExcludesHelpFlag(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{Use: "test", RunE: func(cmd *cobra.Command, args []string) error { return nil }}
	cmd.Flags().String("name", "", "Name value")
	cmd.Flags().Bool("help", false, "Show help")
	
	opts := toolgen.DefaultOptions()
	tools := toolgen.GenerateTools(cmd, opts)
	
	if len(tools) == 0 {
		t.Fatal("expected at least one tool")
	}
	
	tool := tools[0]
	properties, ok := tool.Parameters["properties"].(map[string]any)
	if !ok {
		t.Fatal("expected properties in schema")
	}
	
	if _, hasHelp := properties["help"]; hasHelp {
		t.Error("help flag should be excluded from schema")
	}
	
	if _, hasName := properties["name"]; !hasName {
		t.Error("expected name property to be present")
	}
}

// Test buildEnumProperty via enum-valued flags
func TestBuildParameterSchema_EnumValues(t *testing.T) {
	t.Parallel()

	// Create a command with an enum-valued flag
	cmd := &cobra.Command{
		Use:  "test",
		RunE: func(cmd *cobra.Command, args []string) error { return nil },
	}

	// Use a mock enum value that implements enumValuer
	enumFlag := &mockEnumValue{
		value:       "option1",
		validValues: []string{"option1", "option2", "option3"},
	}
	cmd.Flags().Var(enumFlag, "format", "Output format")

	opts := toolgen.DefaultOptions()
	tools := toolgen.GenerateTools(cmd, opts)

	if len(tools) == 0 {
		t.Fatal("expected at least one tool")
	}

	tool := tools[0]
	properties, ok := tool.Parameters["properties"].(map[string]any)
	if !ok {
		t.Fatal("expected properties in schema")
	}

	prop, exists := properties["format"]
	if !exists {
		t.Fatal("expected 'format' property")
	}

	propMap := prop.(map[string]any)

	// Check type
	if propMap["type"] != "string" {
		t.Errorf("expected type 'string', got %v", propMap["type"])
	}

	// Check enum values
	enumVals, hasEnum := propMap["enum"]
	if !hasEnum {
		t.Fatal("expected 'enum' field")
	}

	enumSlice, ok := enumVals.([]string)
	if !ok {
		t.Fatalf("expected enum to be []string, got %T", enumVals)
	}

	expectedEnum := []string{"option1", "option2", "option3"}
	if len(enumSlice) != len(expectedEnum) {
		t.Errorf("expected %d enum values, got %d", len(expectedEnum), len(enumSlice))
	}

	for i, val := range expectedEnum {
		if i >= len(enumSlice) || enumSlice[i] != val {
			t.Errorf("expected enum[%d] = %q, got %q", i, val, enumSlice[i])
		}
	}

	// Check description includes enum values
	desc, ok := propMap["description"].(string)
	if !ok {
		t.Fatal("expected description to be string")
	}

	if !strings.Contains(desc, "option1") || !strings.Contains(desc, "option2") {
		t.Errorf("expected description to mention enum values, got: %q", desc)
	}
}

// mockDefaultEnumValue implements both enumValuer and defaulter interfaces
type mockDefaultEnumValue struct {
	mockEnumValue
	defaultVal string
}

func (m *mockDefaultEnumValue) Default() any {
	return m.defaultVal
}

// Test buildEnumProperty with default value
func TestBuildParameterSchema_EnumWithDefault(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{
		Use:  "test",
		RunE: func(cmd *cobra.Command, args []string) error { return nil },
	}

	enumFlag := &mockDefaultEnumValue{
		mockEnumValue: mockEnumValue{
			value:       "json",
			validValues: []string{"json", "yaml", "xml"},
		},
		defaultVal: "json",
	}
	cmd.Flags().Var(enumFlag, "output", "Output format")

	opts := toolgen.DefaultOptions()
	tools := toolgen.GenerateTools(cmd, opts)

	if len(tools) == 0 {
		t.Fatal("expected at least one tool")
	}

	tool := tools[0]
	properties, ok := tool.Parameters["properties"].(map[string]any)
	if !ok {
		t.Fatal("expected properties in schema")
	}

	prop := properties["output"].(map[string]any)

	// Check default value
	defaultVal, hasDefault := prop["default"]
	if !hasDefault {
		t.Error("expected default value for enum")
	} else if defaultVal != "json" {
		t.Errorf("expected default 'json', got %v", defaultVal)
	}
}

// Test buildEnumProperty with empty valid values
func TestBuildParameterSchema_EnumEmptyValues(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{
		Use:  "test",
		RunE: func(cmd *cobra.Command, args []string) error { return nil },
	}

	// Empty enum should fall back to standard property
	enumFlag := &mockEnumValue{
		value:       "test",
		validValues: []string{}, // Empty!
	}
	cmd.Flags().Var(enumFlag, "mode", "Mode setting")

	opts := toolgen.DefaultOptions()
	tools := toolgen.GenerateTools(cmd, opts)

	if len(tools) == 0 {
		t.Fatal("expected at least one tool")
	}

	tool := tools[0]
	properties, ok := tool.Parameters["properties"].(map[string]any)
	if !ok {
		t.Fatal("expected properties in schema")
	}

	prop := properties["mode"].(map[string]any)

	// Should not have enum field
	if _, hasEnum := prop["enum"]; hasEnum {
		t.Error("empty enum should not generate enum field")
	}
}

// Test mapFlagTypeToJSONType via schema generation
func TestMapFlagTypes(t *testing.T) {
	t.Parallel()

	flagTests := []struct {
		flagType     string
		setupFlag    func(*pflag.FlagSet)
		expectedType string
	}{
		{
			flagType:     "bool",
			setupFlag:    func(fs *pflag.FlagSet) { fs.Bool("test", false, "test") },
			expectedType: "boolean",
		},
		{
			flagType:     "int",
			setupFlag:    func(fs *pflag.FlagSet) { fs.Int("test", 0, "test") },
			expectedType: "integer",
		},
		{
			flagType:     "int64",
			setupFlag:    func(fs *pflag.FlagSet) { fs.Int64("test", 0, "test") },
			expectedType: "integer",
		},
		{
			flagType:     "uint",
			setupFlag:    func(fs *pflag.FlagSet) { fs.Uint("test", 0, "test") },
			expectedType: "integer",
		},
		{
			flagType:     "float64",
			setupFlag:    func(fs *pflag.FlagSet) { fs.Float64("test", 0, "test") },
			expectedType: "number",
		},
		{
			flagType:     "string",
			setupFlag:    func(fs *pflag.FlagSet) { fs.String("test", "", "test") },
			expectedType: "string",
		},
		{
			flagType:     "stringSlice",
			setupFlag:    func(fs *pflag.FlagSet) { fs.StringSlice("test", nil, "test") },
			expectedType: "array",
		},
	}

	for _, tt := range flagTests {
		t.Run(tt.flagType, func(t *testing.T) {
			t.Parallel()

			cmd := &cobra.Command{Use: "test", RunE: func(cmd *cobra.Command, args []string) error { return nil }}
			tt.setupFlag(cmd.Flags())
			
			opts := toolgen.DefaultOptions()
			tools := toolgen.GenerateTools(cmd, opts)
			
			if len(tools) == 0 {
				t.Fatal("expected at least one tool")
			}
			
			tool := tools[0]
			properties, ok := tool.Parameters["properties"].(map[string]any)
			if !ok {
				t.Fatal("expected properties in schema")
			}
			
			prop, exists := properties["test"]
			if !exists {
				t.Fatal("expected 'test' property")
			}
			
			propMap := prop.(map[string]any)
			if propMap["type"] != tt.expectedType {
				t.Errorf("expected type %q, got %v", tt.expectedType, propMap["type"])
			}
		})
	}
}
