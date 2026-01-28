package toolgen_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/ai/toolgen"
)

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
