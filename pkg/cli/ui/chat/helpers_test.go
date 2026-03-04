package chat_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/cli/ui/chat"
	copilot "github.com/github/copilot-sdk/go"
)

// TestFormatPermissionKind tests formatting of permission kinds to human-readable names.
func TestFormatPermissionKind_AllKinds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		kind     string
		expected string
	}{
		{name: "shell", kind: "shell", expected: "Shell Command"},
		{name: "file_edit", kind: "file_edit", expected: "File Edit"},
		{name: "fileEdit", kind: "fileEdit", expected: "File Edit"},
		{name: "file_read", kind: "file_read", expected: "File Read"},
		{name: "fileRead", kind: "fileRead", expected: "File Read"},
		{name: "file_write", kind: "file_write", expected: "File Write"},
		{name: "fileWrite", kind: "fileWrite", expected: "File Write"},
		{name: "terminal", kind: "terminal", expected: "Terminal"},
		{name: "browser", kind: "browser", expected: "Browser"},
		{name: "network", kind: "network", expected: "Network Request"},
		{name: "empty kind", kind: "", expected: "Unknown Operation"},
		{name: "custom kind", kind: "custom_action", expected: "Custom Action"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result := chat.ExportFormatPermissionKind(tc.kind)
			if result != tc.expected {
				t.Errorf("formatPermissionKind(%q) = %q, want %q", tc.kind, result, tc.expected)
			}
		})
	}
}

// TestExtractPermissionDetails tests permission detail extraction from SDK requests.
func TestExtractPermissionDetails(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		request     copilot.PermissionRequest
		wantTool    string
		wantCommand string
	}{
		{
			name: "command field",
			request: copilot.PermissionRequest{
				Kind:  "shell",
				Extra: map[string]any{"command": "rm -rf /tmp/test"},
			},
			wantTool:    "Shell Command",
			wantCommand: "rm -rf /tmp/test",
		},
		{
			name: "cmd field",
			request: copilot.PermissionRequest{
				Kind:  "shell",
				Extra: map[string]any{"cmd": "echo hello"},
			},
			wantTool:    "Shell Command",
			wantCommand: "echo hello",
		},
		{
			name: "path field",
			request: copilot.PermissionRequest{
				Kind:  "file_edit",
				Extra: map[string]any{"path": "/etc/config.yaml"},
			},
			wantTool:    "File Edit",
			wantCommand: "/etc/config.yaml",
		},
		{
			name: "nested execution",
			request: copilot.PermissionRequest{
				Kind: "shell",
				Extra: map[string]any{
					"execution": map[string]any{
						"command": "docker ps",
					},
				},
			},
			wantTool:    "Shell Command",
			wantCommand: "docker ps",
		},
		{
			name: "fallback to non-metadata field",
			request: copilot.PermissionRequest{
				Kind:  "shell",
				Extra: map[string]any{"description": "running tests"},
			},
			wantTool:    "Shell Command",
			wantCommand: "running tests",
		},
		{
			name: "empty extra falls back to kind",
			request: copilot.PermissionRequest{
				Kind:  "browser",
				Extra: map[string]any{},
			},
			wantTool:    "Browser",
			wantCommand: "browser",
		},
		{
			name: "command as array",
			request: copilot.PermissionRequest{
				Kind: "shell",
				Extra: map[string]any{
					"command": []any{"npm", "install"},
				},
			},
			wantTool:    "Shell Command",
			wantCommand: "npm install",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			toolName, command := chat.ExportExtractPermissionDetails(tc.request)
			if toolName != tc.wantTool {
				t.Errorf("toolName = %q, want %q", toolName, tc.wantTool)
			}

			if command != tc.wantCommand {
				t.Errorf("command = %q, want %q", command, tc.wantCommand)
			}
		})
	}
}

// TestExtractStringValue tests extraction of string values from various types.
func TestExtractStringValue(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		val      any
		expected string
	}{
		{name: "string value", val: "hello", expected: "hello"},
		{name: "empty string", val: "", expected: ""},
		{name: "string array", val: []any{"a", "b", "c"}, expected: "a b c"},
		{name: "empty array", val: []any{}, expected: ""},
		{name: "array with empty strings", val: []any{"", ""}, expected: ""},
		{name: "mixed array", val: []any{"cmd", 123, "arg"}, expected: "cmd arg"},
		{name: "map with command", val: map[string]any{"command": "ls"}, expected: "ls"},
		{name: "map with cmd", val: map[string]any{"cmd": "pwd"}, expected: "pwd"},
		{name: "map without command", val: map[string]any{"other": "val"}, expected: ""},
		{name: "int value", val: 42, expected: ""},
		{name: "nil value", val: nil, expected: ""},
		{name: "bool value", val: true, expected: ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result := chat.ExportExtractStringValue(tc.val)
			if result != tc.expected {
				t.Errorf("extractStringValue(%v) = %q, want %q", tc.val, result, tc.expected)
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

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result := chat.ExportHumanizeToolName(tc.toolName, mappings)
			if result != tc.expected {
				t.Errorf("humanizeToolName(%q) = %q, want %q", tc.toolName, result, tc.expected)
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

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result := chat.ExportCalculatePickerScrollOffset(tc.selectedIndex, tc.totalItems, tc.maxVisible)
			if result != tc.expected {
				t.Errorf("calculatePickerScrollOffset(%d, %d, %d) = %d, want %d",
					tc.selectedIndex, tc.totalItems, tc.maxVisible, result, tc.expected)
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

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result := chat.ExportExtractCommandFromArgs(tc.toolName, tc.args, builders)
			if result != tc.expected {
				t.Errorf("extractCommandFromArgs(%q, %v) = %q, want %q",
					tc.toolName, tc.args, result, tc.expected)
			}
		})
	}
}
