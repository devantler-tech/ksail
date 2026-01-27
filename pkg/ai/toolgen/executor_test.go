package toolgen

import (
	"testing"
)

func TestConsolidatedToolExecution(t *testing.T) {
	t.Parallel()

	// Create a consolidated tool definition manually
	tool := ToolDefinition{
		Name:            "ksail_workload_gen",
		Description:     "Generate Kubernetes resources",
		CommandPath:     "ksail workload gen",
		CommandParts:    []string{"ksail", "workload", "gen"},
		IsConsolidated:  true,
		SubcommandParam: "resource_type",
		Subcommands: map[string]*SubcommandDef{
			"deployment": {
				Name:         "deployment",
				Description:  "Generate a deployment",
				CommandParts: []string{"ksail", "workload", "gen", "deployment"},
				Flags: map[string]*FlagDef{
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
				Flags: map[string]*FlagDef{
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

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			args, err := buildCommandArgs(tool, tt.params)

			if tt.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				} else if tt.errorSubstring != "" && !contains(err.Error(), tt.errorSubstring) {
					t.Errorf("Expected error containing '%s', got: %v", tt.errorSubstring, err)
				}

				return
			}

			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			if !slicesEqual(args, tt.expectedArgs) {
				t.Errorf("Expected args %v, got %v", tt.expectedArgs, args)
			}
		})
	}
}

// Helper function to check if a string contains a substring.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && hasSubstring(s, substr)))
}

func hasSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}

	return false
}

// Helper function to compare slices.
func slicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}

	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}

	return true
}
