package generator_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/cli/cmd"
	"github.com/devantler-tech/ksail/v5/pkg/svc/chat/generator"
)

func TestGenerateToolsFromCommand(t *testing.T) {
	t.Parallel()

	root := cmd.NewRootCmd("test", "abc123", "2024-01-01")
	opts := generator.DefaultOptions()

	tools := generator.GenerateToolsFromCommand(root, opts)

	if len(tools) == 0 {
		t.Fatal("expected at least one tool to be generated")
	}

	t.Logf("Generated %d tools:", len(tools))

	for _, tool := range tools {
		t.Logf("  - %s: %s", tool.Name, truncate(tool.Description, 60))
	}

	// Verify expected tools are present
	expectedTools := map[string]bool{
		"ksail_cluster_create": false,
		"ksail_cluster_delete": false,
		"ksail_cluster_list":   false,
		"ksail_cluster_init":   false,
		"ksail_workload_get":   false,
	}

	for _, tool := range tools {
		if _, ok := expectedTools[tool.Name]; ok {
			expectedTools[tool.Name] = true
		}
	}

	for name, found := range expectedTools {
		if !found {
			t.Errorf("expected tool %q not found", name)
		}
	}

	// Verify excluded tools are NOT present
	excludedTools := []string{
		"ksail_chat",
		"ksail_completion",
	}

	for _, tool := range tools {
		for _, excluded := range excludedTools {
			if tool.Name == excluded {
				t.Errorf("excluded tool %q should not be generated", excluded)
			}
		}
	}
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}

	return s[:maxLen] + "..."
}
