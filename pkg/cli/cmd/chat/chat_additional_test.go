package chat_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/cli/cmd/chat"
	"github.com/devantler-tech/ksail/v7/pkg/toolgen"
	copilot "github.com/github/copilot-sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIsExitCommand verifies that all exit commands are recognised.
func TestIsExitCommand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input    string
		expected bool
	}{
		{"exit", true},
		{"Exit", true},
		{"EXIT", true},
		{"quit", true},
		{"Quit", true},
		{"q", true},
		{"Q", true},
		{"/exit", true},
		{"/quit", true},
		{"/Exit", true},
		{"/Quit", true},
		{"hello", false},
		{"", false},
		{"exits", false},
		{"quitting", false},
	}

	isExit := chat.GetIsExitCommand()

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.expected, isExit(tc.input))
		})
	}
}

// TestGetToolName verifies tool name extraction from session events.
func TestGetToolName(t *testing.T) {
	t.Parallel()

	getToolName := chat.GetToolNameFn()

	t.Run("returns tool name when present", func(t *testing.T) {
		t.Parallel()

		event := copilot.SessionEvent{
			Data: &copilot.ToolExecutionStartData{
				ToolName: "my-tool",
			},
		}

		assert.Equal(t, "my-tool", getToolName(event))
	})

	t.Run("returns unknown when nil", func(t *testing.T) {
		t.Parallel()

		event := copilot.SessionEvent{}

		assert.Equal(t, "unknown", getToolName(event))
	})
}

// TestFormatArgsMap verifies argument formatting.
func TestFormatArgsMap(t *testing.T) {
	t.Parallel()

	formatArgs := chat.GetFormatArgsMap()

	t.Run("empty map returns empty string", func(t *testing.T) {
		t.Parallel()

		assert.Empty(t, formatArgs(nil))
		assert.Empty(t, formatArgs(map[string]any{}))
	})

	t.Run("single arg formatted", func(t *testing.T) {
		t.Parallel()

		result := formatArgs(map[string]any{"name": "test"})
		assert.Equal(t, "name=test", result)
	})

	t.Run("multiple args sorted", func(t *testing.T) {
		t.Parallel()

		result := formatArgs(map[string]any{
			"zzz":  "last",
			"aaa":  "first",
			"name": "middle",
		})
		assert.Equal(t, "aaa=first, name=middle, zzz=last", result)
	})
}

// TestGetToolArgs verifies tool arguments extraction from session events.
func TestGetToolArgs(t *testing.T) {
	t.Parallel()

	getToolArgs := chat.GetToolArgsFn()

	t.Run("nil arguments returns empty", func(t *testing.T) {
		t.Parallel()

		event := copilot.SessionEvent{}
		assert.Empty(t, getToolArgs(event))
	})

	t.Run("non-map arguments returns empty", func(t *testing.T) {
		t.Parallel()

		event := copilot.SessionEvent{
			Data: &copilot.ToolExecutionStartData{Arguments: "string-arg"},
		}
		assert.Empty(t, getToolArgs(event))
	})

	t.Run("empty map returns empty", func(t *testing.T) {
		t.Parallel()

		event := copilot.SessionEvent{
			Data: &copilot.ToolExecutionStartData{Arguments: map[string]any{}},
		}
		assert.Empty(t, getToolArgs(event))
	})

	t.Run("map args formatted with parentheses", func(t *testing.T) {
		t.Parallel()

		event := copilot.SessionEvent{
			Data: &copilot.ToolExecutionStartData{
				Arguments: map[string]any{"name": "cluster"},
			},
		}
		result := getToolArgs(event)
		assert.Equal(t, " (name=cluster)", result)
	})
}

// TestInjectForceFlag verifies force flag injection.
func TestInjectForceFlag(t *testing.T) {
	t.Parallel()

	injectForce := chat.GetInjectForceFlag()

	t.Run("injects force into existing args", func(t *testing.T) {
		t.Parallel()

		invocation := copilot.ToolInvocation{
			Arguments: map[string]any{"name": "test"},
		}
		result := injectForce(invocation)
		args, ok := result.Arguments.(map[string]any)
		require.True(t, ok)
		assert.Equal(t, true, args["force"])
		assert.Equal(t, "test", args["name"])
	})

	t.Run("injects force into nil args", func(t *testing.T) {
		t.Parallel()

		invocation := copilot.ToolInvocation{Arguments: nil}
		result := injectForce(invocation)
		args, ok := result.Arguments.(map[string]any)
		require.True(t, ok)
		assert.Equal(t, true, args["force"])
	})

	t.Run("injects force into non-map args", func(t *testing.T) {
		t.Parallel()

		invocation := copilot.ToolInvocation{Arguments: "string"}
		result := injectForce(invocation)
		args, ok := result.Arguments.(map[string]any)
		require.True(t, ok)
		assert.Equal(t, true, args["force"])
	})
}

// TestToolSupportsForce verifies tool force support checking.
//
//nolint:funlen // table-driven test
func TestToolSupportsForce(t *testing.T) {
	t.Parallel()

	supportsForce := chat.GetToolSupportsForce()

	tests := []struct {
		name     string
		metadata map[string]toolgen.ToolDefinition
		toolName string
		expected bool
	}{
		{
			name:     "nil metadata returns false",
			metadata: nil,
			toolName: "tool",
			expected: false,
		},
		{
			name:     "missing tool returns false",
			metadata: map[string]toolgen.ToolDefinition{},
			toolName: "missing",
			expected: false,
		},
		{
			name: "nil parameters returns false",
			metadata: map[string]toolgen.ToolDefinition{
				"tool": {Name: "tool", Parameters: nil},
			},
			toolName: "tool",
			expected: false,
		},
		{
			name: "no properties key returns false",
			metadata: map[string]toolgen.ToolDefinition{
				"tool": {Name: "tool", Parameters: map[string]any{"type": "object"}},
			},
			toolName: "tool",
			expected: false,
		},
		{
			name: "properties not a map returns false",
			metadata: map[string]toolgen.ToolDefinition{
				"tool": {Name: "tool", Parameters: map[string]any{
					"properties": "not-a-map",
				}},
			},
			toolName: "tool",
			expected: false,
		},
		{
			name: "no force property returns false",
			metadata: map[string]toolgen.ToolDefinition{
				"tool": {Name: "tool", Parameters: map[string]any{
					"properties": map[string]any{
						"name": map[string]any{"type": "string"},
					},
				}},
			},
			toolName: "tool",
			expected: false,
		},
		{
			name: "force property returns true",
			metadata: map[string]toolgen.ToolDefinition{
				"tool": {Name: "tool", Parameters: map[string]any{
					"properties": map[string]any{
						"force": map[string]any{"type": "boolean"},
					},
				}},
			},
			toolName: "tool",
			expected: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.expected, supportsForce(tc.metadata, tc.toolName))
		})
	}
}

// TestValidatePathAccess verifies path sandboxing logic.
//
//nolint:funlen // Table-driven test coverage is naturally long.
func TestValidatePathAccess(t *testing.T) {
	t.Parallel()

	validate := chat.GetValidatePathAccess()
	root := t.TempDir()

	// Create a file in the root to make paths resolvable
	innerDir := filepath.Join(root, "sub")
	require.NoError(t, os.MkdirAll(innerDir, 0o750))

	t.Run("empty root allows everything", func(t *testing.T) {
		t.Parallel()

		input := copilot.PreToolUseHookInput{
			ToolArgs: map[string]any{"path": "/etc/passwd"},
		}
		output, err := validate(input, "")
		require.NoError(t, err)
		assert.Nil(t, output)
	})

	t.Run("no args returns nil", func(t *testing.T) {
		t.Parallel()

		input := copilot.PreToolUseHookInput{ToolArgs: nil}
		output, err := validate(input, root)
		require.NoError(t, err)
		assert.Nil(t, output)
	})

	t.Run("non-map args returns nil", func(t *testing.T) {
		t.Parallel()

		input := copilot.PreToolUseHookInput{ToolArgs: "string"}
		output, err := validate(input, root)
		require.NoError(t, err)
		assert.Nil(t, output)
	})

	t.Run("empty args returns nil", func(t *testing.T) {
		t.Parallel()

		input := copilot.PreToolUseHookInput{ToolArgs: map[string]any{}}
		output, err := validate(input, root)
		require.NoError(t, err)
		assert.Nil(t, output)
	})

	t.Run("path within root allowed", func(t *testing.T) {
		t.Parallel()

		input := copilot.PreToolUseHookInput{
			ToolArgs: map[string]any{"path": filepath.Join(root, "sub", "file.txt")},
		}
		output, err := validate(input, root)
		require.NoError(t, err)
		assert.Nil(t, output)
	})

	t.Run("path outside root denied", func(t *testing.T) {
		t.Parallel()

		input := copilot.PreToolUseHookInput{
			ToolArgs: map[string]any{"path": "/etc/passwd"},
		}
		output, err := validate(input, root)
		require.NoError(t, err)
		require.NotNil(t, output)
		assert.Equal(t, "deny", output.PermissionDecision)
		assert.Contains(t, output.PermissionDecisionReason, "outside the project directory")
	})

	t.Run("filePath key also checked", func(t *testing.T) {
		t.Parallel()

		input := copilot.PreToolUseHookInput{
			ToolArgs: map[string]any{"filePath": "/etc/passwd"},
		}
		output, err := validate(input, root)
		require.NoError(t, err)
		require.NotNil(t, output)
		assert.Equal(t, "deny", output.PermissionDecision)
	})

	t.Run("non-path args ignored", func(t *testing.T) {
		t.Parallel()

		input := copilot.PreToolUseHookInput{
			ToolArgs: map[string]any{"name": "test"},
		}
		output, err := validate(input, root)
		require.NoError(t, err)
		assert.Nil(t, output)
	})

	t.Run("empty path value ignored", func(t *testing.T) {
		t.Parallel()

		input := copilot.PreToolUseHookInput{
			ToolArgs: map[string]any{"path": ""},
		}
		output, err := validate(input, root)
		require.NoError(t, err)
		assert.Nil(t, output)
	})

	t.Run("non-string path value ignored", func(t *testing.T) {
		t.Parallel()

		input := copilot.PreToolUseHookInput{
			ToolArgs: map[string]any{"path": 42},
		}
		output, err := validate(input, root)
		require.NoError(t, err)
		assert.Nil(t, output)
	})
}

// TestBuildPreToolUseHook verifies that the hook builder creates a working hook.
func TestBuildPreToolUseHook(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	hook := chat.BuildPreToolUseHook(root)

	require.NotNil(t, hook)

	// Test that the hook denies outside paths
	input := copilot.PreToolUseHookInput{
		ToolArgs: map[string]any{"path": "/etc/passwd"},
	}
	output, err := hook(input, copilot.HookInvocation{})
	require.NoError(t, err)
	require.NotNil(t, output)
	assert.Equal(t, "deny", output.PermissionDecision)
}
