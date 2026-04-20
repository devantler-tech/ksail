package chat_test

import (
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/cli/ui/chat"
	copilot "github.com/github/copilot-sdk/go"
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
		ConfigFileName:           "nonexistent-config-for-test.yaml",
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

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := builder(testCase.args)
			if result != testCase.expected {
				t.Errorf("got %q, want %q", result, testCase.expected)
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
		{
			name:     "with name",
			args:     map[string]any{"name": "my-cluster"},
			expected: "ksail cluster info --name my-cluster",
		},
		{name: "empty name", args: map[string]any{"name": ""}, expected: "ksail cluster info"},
		{
			name:     "with name and output",
			args:     map[string]any{"name": "prod", "output": "yaml"},
			expected: "ksail cluster info --name prod",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := builder(testCase.args)
			if result != testCase.expected {
				t.Errorf("got %q, want %q", result, testCase.expected)
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
		{
			name:     "resource only",
			args:     map[string]any{"resource": "pods"},
			expected: "ksail workload get pods",
		},
		{
			name: "resource with name", args: map[string]any{"resource": "pods", "name": "nginx"},
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
				"resource": "deployments", "name": "nginx", "namespace": "prod", "output": "json",
			},
			expected: "ksail workload get deployments nginx -n prod -o json",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := builder(testCase.args)
			if result != testCase.expected {
				t.Errorf("got %q, want %q", result, testCase.expected)
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
		{
			name:     "with path",
			args:     map[string]any{"path": "/etc/config"},
			expected: "cat /etc/config",
		},
		{
			name:     "path with spaces",
			args:     map[string]any{"path": "/tmp/my file.txt"},
			expected: "cat /tmp/my file.txt",
		},
		{
			name:     "ignores extra keys",
			args:     map[string]any{"path": "/a", "other": "b"},
			expected: "cat /a",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := builder(testCase.args)
			if result != testCase.expected {
				t.Errorf("got %q, want %q", result, testCase.expected)
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

		for _, testCase := range tests {
			t.Run(toolName+"/"+testCase.name, func(t *testing.T) {
				t.Parallel()

				result := builder(testCase.args)
				if result != testCase.expected {
					t.Errorf("got %q, want %q", result, testCase.expected)
				}
			})
		}
	}
}

// TestBuildSystemSections_Empty tests BuildSystemSections with empty config.
func TestBuildSystemSections_Empty(t *testing.T) {
	t.Parallel()

	sections := chat.BuildSystemSections(chat.SystemContextConfig{})

	if len(sections) != 0 {
		t.Errorf("expected no sections for empty config, got %d", len(sections))
	}
}

// TestBuildSystemSections_WithIdentity tests that Identity maps to SectionIdentity with Replace action.
func TestBuildSystemSections_WithIdentity(t *testing.T) {
	t.Parallel()

	sections := chat.BuildSystemSections(chat.SystemContextConfig{
		Identity: "You are a Kubernetes assistant.",
	})

	section, ok := sections[copilot.SectionIdentity]
	if !ok {
		t.Fatal("expected SectionIdentity key in sections")
	}

	if section.Action != copilot.SectionActionReplace {
		t.Errorf("expected SectionActionReplace, got %q", section.Action)
	}

	if section.Content != "You are a Kubernetes assistant." {
		t.Errorf("unexpected content: %q", section.Content)
	}
}

// TestBuildSystemSections_WithWorkingDir tests that IncludeWorkingDirContext maps to
// SectionEnvironmentContext with Append action.
func TestBuildSystemSections_WithWorkingDir(t *testing.T) {
	t.Parallel()

	sections := chat.BuildSystemSections(chat.SystemContextConfig{
		IncludeWorkingDirContext: true,
		ConfigFileName:           "nonexistent-config-for-test.yaml",
	})

	section, ok := sections[copilot.SectionEnvironmentContext]
	if !ok {
		t.Fatal("expected SectionEnvironmentContext key in sections")
	}

	if section.Action != copilot.SectionActionAppend {
		t.Errorf("expected SectionActionAppend, got %q", section.Action)
	}

	if !strings.Contains(section.Content, "<working_directory>") {
		t.Error("expected <working_directory> tag in environment context")
	}
}

// TestBuildSystemSections_WithDocumentation tests that Documentation maps to
// SectionCustomInstructions with Append action.
func TestBuildSystemSections_WithDocumentation(t *testing.T) {
	t.Parallel()

	sections := chat.BuildSystemSections(chat.SystemContextConfig{
		Documentation: "# KSail Docs\nUsage info here.",
	})

	section, ok := sections[copilot.SectionCustomInstructions]
	if !ok {
		t.Fatal("expected SectionCustomInstructions key in sections")
	}

	if section.Action != copilot.SectionActionAppend {
		t.Errorf("expected SectionActionAppend, got %q", section.Action)
	}

	if !strings.Contains(section.Content, "<documentation>") {
		t.Error("expected <documentation> tag in custom instructions content")
	}

	if !strings.Contains(section.Content, "KSail Docs") {
		t.Error("expected documentation content in custom instructions")
	}
}

// TestBuildSystemSections_WithCLIHelp tests that CLIHelp maps to SectionCustomInstructions.
func TestBuildSystemSections_WithCLIHelp(t *testing.T) {
	t.Parallel()

	sections := chat.BuildSystemSections(chat.SystemContextConfig{
		CLIHelp: "ksail cluster init [flags]",
	})

	section, ok := sections[copilot.SectionCustomInstructions]
	if !ok {
		t.Fatal("expected SectionCustomInstructions key in sections")
	}

	if !strings.Contains(section.Content, "<cli_help>") {
		t.Error("expected <cli_help> tag in custom instructions content")
	}

	if !strings.Contains(section.Content, "ksail cluster init") {
		t.Error("expected CLI help content in custom instructions")
	}
}

// TestBuildSystemSections_WithInstructions tests that Instructions maps to SectionCustomInstructions.
func TestBuildSystemSections_WithInstructions(t *testing.T) {
	t.Parallel()

	sections := chat.BuildSystemSections(chat.SystemContextConfig{
		Instructions: "Always respond in JSON format.",
	})

	section, ok := sections[copilot.SectionCustomInstructions]
	if !ok {
		t.Fatal("expected SectionCustomInstructions key in sections")
	}

	if !strings.Contains(section.Content, "Always respond in JSON format.") {
		t.Error("expected instructions content in custom instructions")
	}
}

// TestBuildSystemSections_AllFields tests BuildSystemSections with all fields populated.
func TestBuildSystemSections_AllFields(t *testing.T) {
	t.Parallel()

	sections := chat.BuildSystemSections(chat.SystemContextConfig{
		Identity:                 "KSail Assistant",
		Documentation:            "Some docs",
		CLIHelp:                  "ksail --help",
		Instructions:             "Be helpful",
		IncludeWorkingDirContext: true,
		ConfigFileName:           "nonexistent-config-for-test.yaml",
	})

	// Should have all three section keys
	if _, ok := sections[copilot.SectionIdentity]; !ok {
		t.Error("expected SectionIdentity key")
	}

	if _, ok := sections[copilot.SectionEnvironmentContext]; !ok {
		t.Error("expected SectionEnvironmentContext key")
	}

	if _, ok := sections[copilot.SectionCustomInstructions]; !ok {
		t.Error("expected SectionCustomInstructions key")
	}

	// Verify identity uses Replace
	if sections[copilot.SectionIdentity].Action != copilot.SectionActionReplace {
		t.Error("expected identity to use SectionActionReplace")
	}

	// Verify environment context uses Append
	if sections[copilot.SectionEnvironmentContext].Action != copilot.SectionActionAppend {
		t.Error("expected environment context to use SectionActionAppend")
	}

	// Verify custom instructions uses Append and combines all content
	custom := sections[copilot.SectionCustomInstructions]
	if custom.Action != copilot.SectionActionAppend {
		t.Error("expected custom instructions to use SectionActionAppend")
	}

	if !strings.Contains(custom.Content, "<documentation>") {
		t.Error("expected documentation in custom instructions")
	}

	if !strings.Contains(custom.Content, "<cli_help>") {
		t.Error("expected cli_help in custom instructions")
	}

	if !strings.Contains(custom.Content, "Be helpful") {
		t.Error("expected instructions text in custom instructions")
	}
}

// TestBuildSystemSections_NoIdentity_OnlyCustom tests that custom instructions section
// is present even without identity.
func TestBuildSystemSections_NoIdentity_OnlyCustom(t *testing.T) {
	t.Parallel()

	sections := chat.BuildSystemSections(chat.SystemContextConfig{
		Documentation: "docs only",
	})

	if _, ok := sections[copilot.SectionIdentity]; ok {
		t.Error("expected no SectionIdentity key when identity is empty")
	}

	if _, ok := sections[copilot.SectionCustomInstructions]; !ok {
		t.Fatal("expected SectionCustomInstructions key")
	}
}

// TestBuildSystemSections_WorkingDirDisabled tests that SectionEnvironmentContext is absent
// when IncludeWorkingDirContext is false.
func TestBuildSystemSections_WorkingDirDisabled(t *testing.T) {
	t.Parallel()

	sections := chat.BuildSystemSections(chat.SystemContextConfig{
		Identity:                 "test",
		IncludeWorkingDirContext: false,
	})

	if _, ok := sections[copilot.SectionEnvironmentContext]; ok {
		t.Error("expected no SectionEnvironmentContext key when working dir context is disabled")
	}
}
