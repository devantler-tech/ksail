package toolgen_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/toolgen"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToolParametersFromJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		json        string
		expected    map[string]any
		expectError bool
	}{
		{"empty object", `{}`, map[string]any{}, false},
		{"string value", `{"name": "test"}`, map[string]any{"name": "test"}, false},
		{"boolean value", `{"force": true}`, map[string]any{"force": true}, false},
		{"numeric value", `{"count": 3}`, map[string]any{"count": float64(3)}, false},
		{"array value", `{"args": ["a", "b"]}`, map[string]any{"args": []any{"a", "b"}}, false},
		{"malformed JSON", `{invalid}`, nil, true},
		{"empty string", ``, nil, true},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			params, err := toolgen.ToolParametersFromJSON(testCase.json)

			if testCase.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, testCase.expected, params)
			}
		})
	}
}

func TestBuildCommandArgs_SimpleCommand(t *testing.T) {
	t.Parallel()

	tool := toolgen.ToolDefinition{
		Name:         "cluster_create",
		CommandParts: []string{"ksail", "cluster", "create"},
	}

	args, err := toolgen.BuildCommandArgs(tool, map[string]any{
		"name": "my-cluster",
	})

	require.NoError(t, err)
	assert.Contains(t, args, "cluster")
	assert.Contains(t, args, "create")
	assert.Contains(t, args, "--name=my-cluster")
}

func TestBuildCommandArgs_BooleanFlags(t *testing.T) {
	t.Parallel()

	tool := toolgen.ToolDefinition{
		Name:         "cluster_delete",
		CommandParts: []string{"ksail", "cluster", "delete"},
	}

	t.Run("true boolean includes flag", func(t *testing.T) {
		t.Parallel()

		args, err := toolgen.BuildCommandArgs(tool, map[string]any{"force": true})

		require.NoError(t, err)
		assert.Contains(t, args, "--force")
	})

	t.Run("false boolean omits flag", func(t *testing.T) {
		t.Parallel()

		args, err := toolgen.BuildCommandArgs(tool, map[string]any{"force": false})

		require.NoError(t, err)

		for _, arg := range args {
			assert.NotContains(t, arg, "force")
		}
	})
}

func TestBuildCommandArgs_ArrayFlag(t *testing.T) {
	t.Parallel()

	tool := toolgen.ToolDefinition{
		Name:         "workload_apply",
		CommandParts: []string{"ksail", "workload", "apply"},
	}

	args, err := toolgen.BuildCommandArgs(tool, map[string]any{
		"filename": []any{"/tmp/a.yaml", "/tmp/b.yaml"},
	})

	require.NoError(t, err)
	assert.Contains(t, args, "--filename=/tmp/a.yaml")
	assert.Contains(t, args, "--filename=/tmp/b.yaml")
}

func TestBuildCommandArgs_PositionalArgs(t *testing.T) {
	t.Parallel()

	tool := toolgen.ToolDefinition{
		Name:         "workload_get",
		CommandParts: []string{"ksail", "workload", "get"},
	}

	args, err := toolgen.BuildCommandArgs(tool, map[string]any{
		"args": []any{"pods", "my-pod"},
	})

	require.NoError(t, err)
	assert.Contains(t, args, "pods")
	assert.Contains(t, args, "my-pod")
}

func TestBuildCommandArgs_ArgsNotArray(t *testing.T) {
	t.Parallel()

	tool := toolgen.ToolDefinition{
		Name:         "workload_get",
		CommandParts: []string{"ksail", "workload", "get"},
	}

	_, err := toolgen.BuildCommandArgs(tool, map[string]any{
		"args": "not-an-array",
	})

	require.ErrorIs(t, err, toolgen.ErrArgsNotArray)
}

func TestBuildCommandArgs_NilValueSkipped(t *testing.T) {
	t.Parallel()

	tool := toolgen.ToolDefinition{
		Name:         "cluster_create",
		CommandParts: []string{"ksail", "cluster", "create"},
	}

	args, err := toolgen.BuildCommandArgs(tool, map[string]any{
		"name":   "my-cluster",
		"config": nil,
	})

	require.NoError(t, err)

	for _, arg := range args {
		assert.NotContains(t, arg, "config")
	}
}

func TestBuildCommandArgs_EmptyParams(t *testing.T) {
	t.Parallel()

	tool := toolgen.ToolDefinition{
		Name:         "cluster_list",
		CommandParts: []string{"ksail", "cluster", "list"},
	}

	args, err := toolgen.BuildCommandArgs(tool, map[string]any{})

	require.NoError(t, err)
	assert.Contains(t, args, "cluster")
	assert.Contains(t, args, "list")
}

//nolint:funlen // Test functions are inherently verbose with test data setup
func TestConsolidatedToolExecution(t *testing.T) {
	t.Parallel()

	// Create a consolidated tool definition manually
	tool := toolgen.ToolDefinition{
		Name:            "ksail_workload_gen",
		Description:     "Generate Kubernetes resources",
		CommandPath:     "ksail workload gen",
		CommandParts:    []string{"ksail", "workload", "gen"},
		IsConsolidated:  true,
		SubcommandParam: "resource_type",
		Subcommands: map[string]*toolgen.SubcommandDef{
			"deployment": {
				Name:         "deployment",
				Description:  "Generate a deployment",
				CommandParts: []string{"ksail", "workload", "gen", "deployment"},
				Flags: map[string]*toolgen.FlagDef{
					"image": {
						Name:        "image",
						Type:        "string",
						Description: "Container image",
					},
				},
			},
			"service": {
				Name:         "service",
				Description:  "Generate a service",
				CommandParts: []string{"ksail", "workload", "gen", "service"},
				Flags: map[string]*toolgen.FlagDef{
					"port": {
						Name:        "port",
						Type:        "integer",
						Description: "Service port",
					},
				},
			},
		},
	}

	tests := []struct {
		name           string
		params         map[string]any
		expectedArgs   []string
		expectError    bool
		errorSubstring string
	}{
		{
			name: "deployment with image",
			params: map[string]any{
				"resource_type": "deployment",
				"image":         "nginx:latest",
			},
			expectedArgs: []string{"workload", "gen", "deployment", "--image=nginx:latest"},
			expectError:  false,
		},
		{
			name: "service with port",
			params: map[string]any{
				"resource_type": "service",
				"port":          8080,
			},
			expectedArgs: []string{"workload", "gen", "service", "--port=8080"},
			expectError:  false,
		},
		{
			name: "missing subcommand parameter",
			params: map[string]any{
				"image": "nginx:latest",
			},
			expectError:    true,
			errorSubstring: "missing or invalid subcommand parameter",
		},
		{
			name: "invalid subcommand",
			params: map[string]any{
				"resource_type": "invalid",
			},
			expectError:    true,
			errorSubstring: "invalid subcommand",
		},
		{
			name: "deployment with inapplicable service flag should filter it out",
			params: map[string]any{
				"resource_type": "deployment",
				"image":         "nginx:latest",
				"port":          8080, // This is a service-only flag, should be filtered
			},
			expectedArgs: []string{"workload", "gen", "deployment", "--image=nginx:latest"},
			expectError:  false,
		},
		{
			name: "service with inapplicable deployment flag should filter it out",
			params: map[string]any{
				"resource_type": "service",
				"port":          8080,
				"image":         "nginx:latest", // This is a deployment-only flag, should be filtered
			},
			expectedArgs: []string{"workload", "gen", "service", "--port=8080"},
			expectError:  false,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			args, err := toolgen.BuildCommandArgs(tool, testCase.params)

			if testCase.expectError {
				if err == nil {
					t.Fatalf("Expected error but got none")
				}

				if testCase.errorSubstring != "" &&
					!strings.Contains(err.Error(), testCase.errorSubstring) {
					t.Fatalf(
						"Expected error containing '%s', got: %v",
						testCase.errorSubstring,
						err,
					)
				}

				// Error was expected and matched; no further checks needed.
				return
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if !slices.Equal(args, testCase.expectedArgs) {
				t.Errorf("Expected args %v, got %v", testCase.expectedArgs, args)
			}
		})
	}
}
