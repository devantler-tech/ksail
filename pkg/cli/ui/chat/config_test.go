package chat_test

import (
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/cli/ui/chat"
)

// TestBuildSystemContext_Empty tests BuildSystemContext with empty config.
func TestBuildSystemContext_Empty(t *testing.T) {
	t.Parallel()

	result, err := chat.BuildSystemContext(chat.SystemContextConfig{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result != "" {
		t.Errorf("expected empty string for empty config, got %q", result)
	}
}

// TestBuildSystemContext_WithIdentity tests BuildSystemContext with identity only.
func TestBuildSystemContext_WithIdentity(t *testing.T) {
	t.Parallel()

	result, err := chat.BuildSystemContext(chat.SystemContextConfig{
		Identity: "You are a Kubernetes assistant.",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(result, "<identity>") {
		t.Error("expected <identity> tag in output")
	}

	if !strings.Contains(result, "You are a Kubernetes assistant.") {
		t.Error("expected identity content in output")
	}

	if !strings.Contains(result, "</identity>") {
		t.Error("expected </identity> closing tag in output")
	}
}

// TestBuildSystemContext_WithDocumentation tests BuildSystemContext with documentation.
func TestBuildSystemContext_WithDocumentation(t *testing.T) {
	t.Parallel()

	result, err := chat.BuildSystemContext(chat.SystemContextConfig{
		Documentation: "# KSail Docs\nUsage info here.",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(result, "<documentation>") {
		t.Error("expected <documentation> tag in output")
	}

	if !strings.Contains(result, "KSail Docs") {
		t.Error("expected documentation content in output")
	}
}

// TestBuildSystemContext_WithCLIHelp tests BuildSystemContext with CLI help text.
func TestBuildSystemContext_WithCLIHelp(t *testing.T) {
	t.Parallel()

	result, err := chat.BuildSystemContext(chat.SystemContextConfig{
		CLIHelp: "ksail cluster init [flags]",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(result, "<cli_help>") {
		t.Error("expected <cli_help> tag in output")
	}

	if !strings.Contains(result, "ksail cluster init") {
		t.Error("expected CLI help content in output")
	}
}

// TestBuildSystemContext_WithInstructions tests BuildSystemContext with instructions.
func TestBuildSystemContext_WithInstructions(t *testing.T) {
	t.Parallel()

	result, err := chat.BuildSystemContext(chat.SystemContextConfig{
		Instructions: "Always respond in JSON format.",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(result, "Always respond in JSON format.") {
		t.Error("expected instructions content in output")
	}
}

// TestBuildSystemContext_AllFields tests BuildSystemContext with all fields populated.
func TestBuildSystemContext_AllFields(t *testing.T) {
	t.Parallel()

	result, err := chat.BuildSystemContext(chat.SystemContextConfig{
		Identity:      "KSail Assistant",
		Documentation: "Some docs",
		CLIHelp:       "ksail --help",
		Instructions:  "Be helpful",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify all sections are present
	if !strings.Contains(result, "<identity>") {
		t.Error("expected <identity> tag")
	}

	if !strings.Contains(result, "<documentation>") {
		t.Error("expected <documentation> tag")
	}

	if !strings.Contains(result, "<cli_help>") {
		t.Error("expected <cli_help> tag")
	}

	if !strings.Contains(result, "Be helpful") {
		t.Error("expected instructions")
	}

	// Verify ordering: identity before documentation before cli_help
	idIdx := strings.Index(result, "<identity>")
	docIdx := strings.Index(result, "<documentation>")
	cliIdx := strings.Index(result, "<cli_help>")

	if idIdx >= docIdx {
		t.Error("expected identity before documentation")
	}

	if docIdx >= cliIdx {
		t.Error("expected documentation before cli_help")
	}
}

// TestBuildSystemContext_WithWorkingDir tests BuildSystemContext with working directory context.
func TestBuildSystemContext_WithWorkingDir(t *testing.T) {
	t.Parallel()

	result, err := chat.BuildSystemContext(chat.SystemContextConfig{
		IncludeWorkingDirContext: true,
		ConfigFileName:          "nonexistent-config-for-test.yaml",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should include working directory even if config file doesn't exist
	if !strings.Contains(result, "<working_directory>") {
		t.Error("expected <working_directory> tag in output")
	}
}

// TestDefaultThemeConfig tests that defaults are populated.
func TestDefaultThemeConfig(t *testing.T) {
	t.Parallel()

	theme := chat.DefaultThemeConfig()

	if theme.Logo == nil {
		t.Error("expected Logo to be set")
	}

	if theme.Tagline == nil {
		t.Error("expected Tagline to be set")
	}

	if theme.LogoHeight == 0 {
		t.Error("expected LogoHeight to be non-zero")
	}

	if theme.AssistantLabel == "" {
		t.Error("expected AssistantLabel to be set")
	}

	if theme.Placeholder == "" {
		t.Error("expected Placeholder to be set")
	}

	if theme.WelcomeMessage == "" {
		t.Error("expected WelcomeMessage to be set")
	}

	if theme.GoodbyeMessage == "" {
		t.Error("expected GoodbyeMessage to be set")
	}

	if theme.SessionDir == "" {
		t.Error("expected SessionDir to be set")
	}

	// Verify logo is multi-line
	logo := theme.Logo()
	if !strings.Contains(logo, "\n") {
		t.Error("expected multi-line logo")
	}
}

// TestDefaultToolDisplayConfig tests that tool display defaults are populated.
func TestDefaultToolDisplayConfig(t *testing.T) {
	t.Parallel()

	config := chat.DefaultToolDisplayConfig()

	if config.NameMappings == nil {
		t.Error("expected NameMappings to be set")
	}

	if config.CommandBuilders == nil {
		t.Error("expected CommandBuilders to be set")
	}

	// Verify known tool mappings
	if _, ok := config.NameMappings["bash"]; !ok {
		t.Error("expected 'bash' in NameMappings")
	}

	if _, ok := config.NameMappings["read_file"]; !ok {
		t.Error("expected 'read_file' in NameMappings")
	}
}

// TestCommandBuilder_ClusterList tests the cluster list command builder.
func TestCommandBuilder_ClusterList(t *testing.T) {
	t.Parallel()

	builders := chat.DefaultToolDisplayConfig().CommandBuilders
	builder, ok := builders["ksail_cluster_list"]

	if !ok {
		t.Fatal("expected 'ksail_cluster_list' command builder")
	}

	tests := []struct {
		name     string
		args     map[string]any
		expected string
	}{
		{name: "no args", args: map[string]any{}, expected: "ksail cluster list"},
		{name: "with all", args: map[string]any{"all": true}, expected: "ksail cluster list --all"},
		{name: "all false", args: map[string]any{"all": false}, expected: "ksail cluster list"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result := builder(tc.args)
			if result != tc.expected {
				t.Errorf("got %q, want %q", result, tc.expected)
			}
		})
	}
}

// TestCommandBuilder_ClusterInfo tests the cluster info command builder.
func TestCommandBuilder_ClusterInfo(t *testing.T) {
	t.Parallel()

	builders := chat.DefaultToolDisplayConfig().CommandBuilders
	builder, ok := builders["ksail_cluster_info"]

	if !ok {
		t.Fatal("expected 'ksail_cluster_info' command builder")
	}

	tests := []struct {
		name     string
		args     map[string]any
		expected string
	}{
		{name: "no args", args: map[string]any{}, expected: "ksail cluster info"},
		{name: "with name", args: map[string]any{"name": "my-cluster"}, expected: "ksail cluster info --name my-cluster"},
		{name: "empty name", args: map[string]any{"name": ""}, expected: "ksail cluster info"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result := builder(tc.args)
			if result != tc.expected {
				t.Errorf("got %q, want %q", result, tc.expected)
			}
		})
	}
}

// TestCommandBuilder_WorkloadGet tests the workload get command builder.
func TestCommandBuilder_WorkloadGet(t *testing.T) {
	t.Parallel()

	builders := chat.DefaultToolDisplayConfig().CommandBuilders
	builder, ok := builders["ksail_workload_get"]

	if !ok {
		t.Fatal("expected 'ksail_workload_get' command builder")
	}

	tests := []struct {
		name     string
		args     map[string]any
		expected string
	}{
		{name: "no resource", args: map[string]any{}, expected: ""},
		{name: "empty resource", args: map[string]any{"resource": ""}, expected: ""},
		{name: "resource only", args: map[string]any{"resource": "pods"}, expected: "ksail workload get pods"},
		{
			name:     "resource with name",
			args:     map[string]any{"resource": "pods", "name": "nginx"},
			expected: "ksail workload get pods nginx",
		},
		{
			name:     "resource with namespace",
			args:     map[string]any{"resource": "pods", "namespace": "default"},
			expected: "ksail workload get pods -n default",
		},
		{
			name:     "resource with all namespaces",
			args:     map[string]any{"resource": "pods", "all_namespaces": true},
			expected: "ksail workload get pods -A",
		},
		{
			name:     "resource with output format",
			args:     map[string]any{"resource": "pods", "output": "yaml"},
			expected: "ksail workload get pods -o yaml",
		},
		{
			name: "all options",
			args: map[string]any{
				"resource":  "deployments",
				"name":      "nginx",
				"namespace": "prod",
				"output":    "json",
			},
			expected: "ksail workload get deployments nginx -n prod -o json",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result := builder(tc.args)
			if result != tc.expected {
				t.Errorf("got %q, want %q", result, tc.expected)
			}
		})
	}
}

// TestCommandBuilder_ReadFile tests the read file command builder.
func TestCommandBuilder_ReadFile(t *testing.T) {
	t.Parallel()

	builders := chat.DefaultToolDisplayConfig().CommandBuilders
	builder, ok := builders["read_file"]

	if !ok {
		t.Fatal("expected 'read_file' command builder")
	}

	tests := []struct {
		name     string
		args     map[string]any
		expected string
	}{
		{name: "no path", args: map[string]any{}, expected: ""},
		{name: "empty path", args: map[string]any{"path": ""}, expected: ""},
		{name: "with path", args: map[string]any{"path": "/etc/config"}, expected: "cat /etc/config"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result := builder(tc.args)
			if result != tc.expected {
				t.Errorf("got %q, want %q", result, tc.expected)
			}
		})
	}
}

// TestCommandBuilder_ListDirectory tests the list directory command builder.
func TestCommandBuilder_ListDirectory(t *testing.T) {
	t.Parallel()

	builders := chat.DefaultToolDisplayConfig().CommandBuilders

	// Both "list_dir" and "list_directory" should map to the same builder
	for _, toolName := range []string{"list_dir", "list_directory"} {
		builder, ok := builders[toolName]
		if !ok {
			t.Fatalf("expected '%s' command builder", toolName)
		}

		tests := []struct {
			name     string
			args     map[string]any
			expected string
		}{
			{name: "no path", args: map[string]any{}, expected: "ls ."},
			{name: "empty path", args: map[string]any{"path": ""}, expected: "ls ."},
			{name: "with path", args: map[string]any{"path": "/home"}, expected: "ls /home"},
		}

		for _, tc := range tests {
			t.Run(toolName+"/"+tc.name, func(t *testing.T) {
				t.Parallel()

				result := builder(tc.args)
				if result != tc.expected {
					t.Errorf("got %q, want %q", result, tc.expected)
				}
			})
		}
	}
}
