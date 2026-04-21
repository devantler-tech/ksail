package chat_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/cli/ui/chat"
	copilot "github.com/github/copilot-sdk/go"
)

// TestFormatPermissionKind tests formatting of permission kinds to human-readable names.
func TestFormatPermissionKind_AllKinds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		kind     copilot.PermissionRequestKind
		expected string
	}{
		{name: "shell", kind: copilot.PermissionRequestKindShell, expected: "Shell Command"},
		{name: "write", kind: copilot.PermissionRequestKindWrite, expected: "File Write"},
		{name: "read", kind: copilot.PermissionRequestKindRead, expected: "File Read"},
		{name: "url", kind: copilot.PermissionRequestKindURL, expected: "URL"},
		{name: "mcp", kind: copilot.PermissionRequestKindMcp, expected: "MCP Tool"},
		{
			name:     "custom-tool",
			kind:     copilot.PermissionRequestKindCustomTool,
			expected: "Custom Tool",
		},
		{name: "memory", kind: copilot.PermissionRequestKindMemory, expected: "Memory"},
		{name: "empty kind", kind: "", expected: "Unknown Operation"},
		{name: "custom kind", kind: "custom_action", expected: "Custom Action"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := chat.ExportFormatPermissionKind(testCase.kind)
			if result != testCase.expected {
				t.Errorf(
					"formatPermissionKind(%q) = %q, want %q",
					testCase.kind,
					result,
					testCase.expected,
				)
			}
		})
	}
}

// TestExtractPermissionDetails_CommandFields tests permission detail extraction for command fields.
func TestExtractPermissionDetails_CommandFields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		request     copilot.PermissionRequest
		wantTool    string
		wantCommand string
	}{
		{
			name: "full command text",
			request: copilot.PermissionRequest{
				Kind:            copilot.PermissionRequestKindShell,
				FullCommandText: new("rm -rf /tmp/test"),
			},
			wantTool:    "Shell Command",
			wantCommand: "rm -rf /tmp/test",
		},
		{
			name: "tool name field",
			request: copilot.PermissionRequest{
				Kind:     copilot.PermissionRequestKindMcp,
				ToolName: new("ksail_cluster_create"),
			},
			wantTool:    "MCP Tool",
			wantCommand: "ksail_cluster_create",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			toolName, command := chat.ExportExtractPermissionDetails(testCase.request)
			if toolName != testCase.wantTool {
				t.Errorf("toolName = %q, want %q", toolName, testCase.wantTool)
			}

			if command != testCase.wantCommand {
				t.Errorf("command = %q, want %q", command, testCase.wantCommand)
			}
		})
	}
}

// TestExtractPermissionDetails_PathAndFallback tests permission detail extraction for path and fallback fields.
func TestExtractPermissionDetails_PathAndFallback(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		request     copilot.PermissionRequest
		wantTool    string
		wantCommand string
	}{
		{
			name: "path field",
			request: copilot.PermissionRequest{
				Kind: copilot.PermissionRequestKindRead,
				Path: new("/etc/config.yaml"),
			},
			wantTool:    "File Read",
			wantCommand: "/etc/config.yaml",
		},
		{
			name: "fileName field",
			request: copilot.PermissionRequest{
				Kind:     copilot.PermissionRequestKindWrite,
				FileName: new("/tmp/output.txt"),
			},
			wantTool:    "File Write",
			wantCommand: "/tmp/output.txt",
		},
		{
			name: "url field",
			request: copilot.PermissionRequest{
				Kind: copilot.PermissionRequestKindURL,
				URL:  new("https://example.com"),
			},
			wantTool:    "URL",
			wantCommand: "https://example.com",
		},
		{
			name:        "no fields falls back to kind",
			request:     copilot.PermissionRequest{Kind: copilot.PermissionRequestKindMemory},
			wantTool:    "Memory",
			wantCommand: "memory",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			toolName, command := chat.ExportExtractPermissionDetails(testCase.request)
			if toolName != testCase.wantTool {
				t.Errorf("toolName = %q, want %q", toolName, testCase.wantTool)
			}

			if command != testCase.wantCommand {
				t.Errorf("command = %q, want %q", command, testCase.wantCommand)
			}
		})
	}
}

// TestHumanizeToolName tests conversion of snake_case tool names to readable format.
func TestHumanizeToolName(t *testing.T) {
	t.Parallel()

	mappings := map[string]string{
		"bash":      "Running command",
		"read_file": "Reading file",
	}

	tests := []struct {
		name     string
		toolName string
		expected string
	}{
		{name: "mapped name", toolName: "bash", expected: "Running command"},
		{name: "mapped file", toolName: "read_file", expected: "Reading file"},
		{name: "unmapped snake_case", toolName: "write_file", expected: "Write File"},
		{name: "unmapped single word", toolName: "deploy", expected: "Deploy"},
		{name: "empty name", toolName: "", expected: ""},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := chat.ExportHumanizeToolName(testCase.toolName, mappings)
			if result != testCase.expected {
				t.Errorf(
					"humanizeToolName(%q) = %q, want %q",
					testCase.toolName,
					result,
					testCase.expected,
				)
			}
		})
	}
}

// TestCalculatePickerScrollOffset tests scroll offset calculation for picker lists.
func TestCalculatePickerScrollOffset(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		selectedIndex int
		totalItems    int
		maxVisible    int
		expected      int
	}{
		{name: "all items visible", selectedIndex: 0, totalItems: 3, maxVisible: 5, expected: 0},
		{name: "first item selected", selectedIndex: 0, totalItems: 10, maxVisible: 5, expected: 0},
		{name: "middle item", selectedIndex: 3, totalItems: 10, maxVisible: 5, expected: 0},
		{name: "near end", selectedIndex: 7, totalItems: 10, maxVisible: 5, expected: 3},
		{name: "last item", selectedIndex: 9, totalItems: 10, maxVisible: 5, expected: 5},
		{name: "single item", selectedIndex: 0, totalItems: 1, maxVisible: 5, expected: 0},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := chat.ExportCalculatePickerScrollOffset(
				testCase.selectedIndex,
				testCase.totalItems,
				testCase.maxVisible,
			)
			if result != testCase.expected {
				t.Errorf(
					"calculatePickerScrollOffset(%d, %d, %d) = %d, want %d",
					testCase.selectedIndex,
					testCase.totalItems,
					testCase.maxVisible,
					result,
					testCase.expected,
				)
			}
		})
	}
}

// TestExtractCommandFromArgs tests command extraction from tool arguments.
func TestExtractCommandFromArgs(t *testing.T) {
	t.Parallel()

	builders := chat.DefaultToolDisplayConfig().CommandBuilders

	tests := []struct {
		name     string
		toolName string
		args     any
		expected string
	}{
		{
			name:     "known tool with valid args",
			toolName: "read_file",
			args:     map[string]any{"path": "/tmp/test"},
			expected: "cat /tmp/test",
		},
		{
			name:     "unknown tool",
			toolName: "unknown_tool",
			args:     map[string]any{"arg": "value"},
			expected: "",
		},
		{
			name:     "non-map args",
			toolName: "read_file",
			args:     "not a map",
			expected: "",
		},
		{
			name:     "nil args",
			toolName: "read_file",
			args:     nil,
			expected: "",
		},
		{
			name:     "cluster list with all",
			toolName: "ksail_cluster_list",
			args:     map[string]any{"all": true},
			expected: "ksail cluster list --all",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := chat.ExportExtractCommandFromArgs(testCase.toolName, testCase.args, builders)
			if result != testCase.expected {
				t.Errorf("extractCommandFromArgs(%q, %v) = %q, want %q",
					testCase.toolName, testCase.args, result, testCase.expected)
			}
		})
	}
}
