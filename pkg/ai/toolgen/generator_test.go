package toolgen_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/ai/toolgen"
	"github.com/devantler-tech/ksail/v5/pkg/cli/cmd"
	"github.com/spf13/cobra"
)

func TestGenerateTools(t *testing.T) {
	t.Parallel()

	root := cmd.NewRootCmd("test", "abc123", "2024-01-01")
	opts := toolgen.DefaultOptions()

	tools := toolgen.GenerateTools(root, opts)

	if len(tools) == 0 {
		t.Fatal("expected at least one tool to be generated")
	}

	t.Logf("Generated %d tools:", len(tools))

	for _, tool := range tools {
		t.Logf("  - %s: %s", tool.Name, truncate(tool.Description, 60))
	}

	// Verify expected tools are present
	expectedTools := map[string]bool{
		"ksail_cluster_read":   false,
		"ksail_cluster_write":  false,
		"ksail_workload_read":  false,
		"ksail_workload_write": false,
		"ksail_cipher_read":    false,
		"ksail_cipher_write":   false,
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
		"ksail_mcp",
	}

	for _, tool := range tools {
		for _, excluded := range excludedTools {
			if tool.Name == excluded {
				t.Errorf("excluded tool %q should not be generated", excluded)
			}
		}
	}
}

func TestToolDefinitionStructure(t *testing.T) {
	t.Parallel()

	root := cmd.NewRootCmd("test", "abc123", "2024-01-01")
	opts := toolgen.DefaultOptions()

	tools := toolgen.GenerateTools(root, opts)

	for _, tool := range tools {
		// Verify basic structure
		if tool.Name == "" {
			t.Error("tool name should not be empty")
		}

		if tool.Description == "" {
			t.Error("tool description should not be empty")
		}

		if tool.CommandPath == "" {
			t.Error("tool CommandPath should not be empty")
		}

		if len(tool.CommandParts) == 0 {
			t.Error("tool CommandParts should not be empty")
		}

		// Verify parameters schema structure
		if tool.Parameters == nil {
			t.Error("tool Parameters should not be nil")
		}

		// Verify schema has expected fields
		if schemaType, ok := tool.Parameters["type"].(string); !ok || schemaType != "object" {
			t.Errorf("expected schema type 'object', got %v", tool.Parameters["type"])
		}

		if _, ok := tool.Parameters["properties"]; !ok {
			t.Error("schema should have 'properties' field")
		}
	}
}

func TestFormatToolName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		commandPath string
		expected    string
	}{
		{
			name:        "single command",
			commandPath: "ksail",
			expected:    "ksail",
		},
		{
			name:        "two level command",
			commandPath: "ksail cluster",
			expected:    "ksail_cluster",
		},
		{
			name:        "three level command",
			commandPath: "ksail cluster create",
			expected:    "ksail_cluster_create",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := toolgen.FormatToolName(testCase.commandPath)
			if result != testCase.expected {
				t.Errorf("expected %q, got %q", testCase.expected, result)
			}
		})
	}
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}

	return s[:maxLen] + "..."
}

func TestConsolidatedToolGeneration(t *testing.T) {
	t.Parallel()

	// Create a parent command with consolidation annotation
	parentCmd := &cobra.Command{
		Use:   "gen",
		Short: "Generate Kubernetes resources",
		Annotations: map[string]string{
			"ai.toolgen.consolidate": "resource_type",
		},
	}

	// Add subcommands
	deploymentCmd := &cobra.Command{
		Use:   "deployment",
		Short: "Generate a deployment",
		RunE: func(_ *cobra.Command, _ []string) error {
			return nil
		},
	}
	deploymentCmd.Flags().String("image", "", "Container image")
	deploymentCmd.Flags().Int32("replicas", 1, "Number of replicas")

	serviceCmd := &cobra.Command{
		Use:   "service",
		Short: "Generate a service",
		RunE: func(_ *cobra.Command, _ []string) error {
			return nil
		},
	}
	serviceCmd.Flags().Int32("port", 80, "Service port")
	serviceCmd.Flags().String("type", "ClusterIP", "Service type")

	parentCmd.AddCommand(deploymentCmd, serviceCmd)

	// Generate tools
	tools := toolgen.GenerateTools(parentCmd, toolgen.DefaultOptions())

	// Verify only one tool was created
	if len(tools) != 1 {
		t.Fatalf("Expected 1 consolidated tool, got %d", len(tools))
	}

	tool := tools[0]

	// Verify tool is marked as consolidated
	if !tool.IsConsolidated {
		t.Error("Tool should be marked as consolidated")
	}

	// Verify subcommand parameter
	if tool.SubcommandParam != "resource_type" {
		t.Errorf("Expected SubcommandParam 'resource_type', got '%s'", tool.SubcommandParam)
	}

	// Verify subcommands were captured
	if len(tool.Subcommands) != 2 {
		t.Fatalf("Expected 2 subcommands, got %d", len(tool.Subcommands))
	}

	if _, exists := tool.Subcommands["deployment"]; !exists {
		t.Error("Missing 'deployment' subcommand")
	}

	if _, exists := tool.Subcommands["service"]; !exists {
		t.Error("Missing 'service' subcommand")
	}
}

//nolint:funlen // Test functions are inherently verbose with test data setup
func TestConsolidatedToolSchema(t *testing.T) {
	t.Parallel()

	// Create a parent command with subcommands
	parentCmd := &cobra.Command{
		Use:   "rollout",
		Short: "Manage rollouts",
		Annotations: map[string]string{
			"ai.toolgen.consolidate": "action",
		},
	}

	restartCmd := &cobra.Command{
		Use:   "restart",
		Short: "Restart a deployment",
		RunE: func(_ *cobra.Command, _ []string) error {
			return nil
		},
	}
	restartCmd.Flags().String("namespace", "", "Namespace")

	statusCmd := &cobra.Command{
		Use:   "status",
		Short: "Show rollout status",
		RunE: func(_ *cobra.Command, _ []string) error {
			return nil
		},
	}
	statusCmd.Flags().String("namespace", "", "Namespace")
	statusCmd.Flags().Bool("watch", false, "Watch for changes")

	parentCmd.AddCommand(restartCmd, statusCmd)

	// Generate tools
	tools := toolgen.GenerateTools(parentCmd, toolgen.DefaultOptions())
	tool := tools[0]

	// Verify parameters schema
	params := tool.Parameters

	// Check that action parameter exists with enum
	properties, validType := params["properties"].(map[string]any)
	if !validType {
		t.Fatal("Properties should be a map")
	}

	actionProp, exists := properties["action"]
	if !exists {
		t.Fatal("Expected 'action' parameter in schema")
	}

	actionMap, isMap := actionProp.(map[string]any)
	if !isMap {
		t.Fatal("Action property should be a map")
	}

	// Verify enum values
	enum, exists := actionMap["enum"]
	if !exists {
		t.Fatal("Expected enum in action parameter")
	}

	enumSlice, isSlice := enum.([]string)
	if !isSlice {
		t.Fatal("Enum should be a string slice")
	}

	if len(enumSlice) != 2 {
		t.Errorf("Expected 2 enum values, got %d", len(enumSlice))
	}

	// Verify flags are merged
	if _, exists := properties["namespace"]; !exists {
		t.Error("Expected 'namespace' flag in schema")
	}

	if _, exists := properties["watch"]; !exists {
		t.Error("Expected 'watch' flag in schema")
	}

	// Verify conditional flag annotation
	watchProp, _ := properties["watch"].(map[string]any)

	watchDesc, _ := watchProp["description"].(string)
	if watchDesc == "" {
		t.Error("Expected description for watch flag")
	}

	// Watch flag should indicate it only applies to status
	// (since it's not in restart)
}

func TestToolCountReduction(t *testing.T) {
	t.Parallel()

	// Create a consolidated gen command with multiple subcommands
	genCmd := &cobra.Command{
		Use:   "gen",
		Short: "Generate resources",
		Annotations: map[string]string{
			"ai.toolgen.consolidate": "resource_type",
		},
	}

	// Add 17 subcommands (like the real gen command)
	subcommandNames := []string{
		"clusterrole", "clusterrolebinding", "configmap", "cronjob",
		"deployment", "helmrelease", "ingress", "job", "namespace",
		"poddisruptionbudget", "priorityclass", "quota", "role",
		"rolebinding", "secret", "service", "serviceaccount",
	}

	for _, name := range subcommandNames {
		subCmd := &cobra.Command{
			Use:   name,
			Short: "Generate " + name,
			RunE: func(_ *cobra.Command, _ []string) error {
				return nil
			},
		}
		genCmd.AddCommand(subCmd)
	}

	// Generate tools with consolidation
	consolidatedTools := toolgen.GenerateTools(genCmd, toolgen.DefaultOptions())

	// Generate tools without consolidation (by removing annotation)
	genCmd.Annotations = nil
	unconsolidatedTools := toolgen.GenerateTools(genCmd, toolgen.DefaultOptions())

	// Verify reduction
	if len(consolidatedTools) != 1 {
		t.Errorf("Expected 1 consolidated tool, got %d", len(consolidatedTools))
	}

	if len(unconsolidatedTools) != 17 {
		t.Errorf("Expected 17 unconsolidated tools, got %d", len(unconsolidatedTools))
	}

	reduction := len(unconsolidatedTools) - len(consolidatedTools)
	if reduction != 16 {
		t.Errorf("Expected reduction of 16 tools, got %d", reduction)
	}
}
