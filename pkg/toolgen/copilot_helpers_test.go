package toolgen_test

import (
	"context"
	"errors"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/toolgen"
	copilot "github.com/github/copilot-sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// errFake is a reusable error value for tests.
var errFake = errors.New("command failed: exit status 1")

// --- buildFullCommand tests ---

func TestBuildFullCommand_NoParams(t *testing.T) {
	t.Parallel()

	result := toolgen.BuildFullCommand("ksail cluster create", map[string]any{})

	assert.Equal(t, "ksail cluster create", result)
}

func TestBuildFullCommand_NilParams(t *testing.T) {
	t.Parallel()

	result := toolgen.BuildFullCommand("ksail cluster create", nil)

	assert.Equal(t, "ksail cluster create", result)
}

func TestBuildFullCommand_WithFlags(t *testing.T) {
	t.Parallel()

	// Single flag
	result := toolgen.BuildFullCommand("ksail cluster create", map[string]any{
		"name": "my-cluster",
	})

	assert.Contains(t, result, "ksail cluster create")
	assert.Contains(t, result, "--name=my-cluster")
}

func TestBuildFullCommand_WithPositionalArgs(t *testing.T) {
	t.Parallel()

	result := toolgen.BuildFullCommand("ksail cluster create", map[string]any{
		"args": []any{"arg1", "arg2"},
	})

	assert.Contains(t, result, "ksail cluster create")
	assert.Contains(t, result, "arg1")
	assert.Contains(t, result, "arg2")
}

func TestBuildFullCommand_WithBoolFlags(t *testing.T) {
	t.Parallel()

	result := toolgen.BuildFullCommand("ksail cluster create", map[string]any{
		"verbose": true,
	})

	assert.Contains(t, result, "--verbose")
}

func TestBuildFullCommand_FalseBoolFlagOmitted(t *testing.T) {
	t.Parallel()

	result := toolgen.BuildFullCommand("ksail cluster create", map[string]any{
		"verbose": false,
	})

	// False bool flags should not appear
	assert.Equal(t, "ksail cluster create", result)
}

// --- buildCopilotResult tests ---

func TestBuildCopilotResult_Success_WithOutput(t *testing.T) {
	t.Parallel()

	result := toolgen.BuildCopilotResult("ksail cluster list", "cluster1\ncluster2", nil)

	assert.Equal(t, "success", result.ResultType)
	assert.Contains(t, result.TextResultForLLM, "Command: ksail cluster list")
	assert.Contains(t, result.TextResultForLLM, "Status: SUCCESS")
	assert.Contains(t, result.TextResultForLLM, "Output:\ncluster1\ncluster2")
	assert.Equal(t, "[SUCCESS] ksail cluster list", result.SessionLog)
	assert.NotNil(t, result.ToolTelemetry)
}

func TestBuildCopilotResult_Success_NoOutput(t *testing.T) {
	t.Parallel()

	result := toolgen.BuildCopilotResult("ksail cluster create", "", nil)

	assert.Equal(t, "success", result.ResultType)
	assert.Contains(t, result.TextResultForLLM, "Status: SUCCESS")
	assert.NotContains(t, result.TextResultForLLM, "Output:")
}

func TestBuildCopilotResult_Failure(t *testing.T) {
	t.Parallel()

	result := toolgen.BuildCopilotResult("ksail cluster create", "", errFake)

	assert.Equal(t, "failure", result.ResultType)
	assert.Contains(t, result.TextResultForLLM, "Command: ksail cluster create")
	assert.Contains(t, result.TextResultForLLM, "Status: FAILED")
	assert.Contains(t, result.TextResultForLLM, "Error: command failed: exit status 1")
	assert.Contains(t, result.SessionLog, "[FAILED] ksail cluster create")
	assert.NotNil(t, result.ToolTelemetry)
}

// --- formatParametersForDisplay tests ---

func TestFormatParametersForDisplay_Empty(t *testing.T) {
	t.Parallel()

	result := toolgen.FormatParametersForDisplay(map[string]any{})

	assert.Empty(t, result)
}

func TestFormatParametersForDisplay_WithFlags(t *testing.T) {
	t.Parallel()

	result := toolgen.FormatParametersForDisplay(map[string]any{
		"name": "my-cluster",
	})

	assert.Contains(t, result, "--name=my-cluster")
}

func TestFormatParametersForDisplay_WithArgsAndFlags(t *testing.T) {
	t.Parallel()

	result := toolgen.FormatParametersForDisplay(map[string]any{
		"args": []any{"pos1"},
		"name": "my-cluster",
	})

	assert.Contains(t, result, "pos1")
	assert.Contains(t, result, "--name=my-cluster")
}

func TestFormatParametersForDisplay_ArrayFlag(t *testing.T) {
	t.Parallel()

	result := toolgen.FormatParametersForDisplay(map[string]any{
		"labels": []any{"app=web", "env=prod"},
	})

	assert.Contains(t, result, "--labels=app=web")
	assert.Contains(t, result, "--labels=env=prod")
}

func TestFormatParametersForDisplay_NilValue(t *testing.T) {
	t.Parallel()

	result := toolgen.FormatParametersForDisplay(map[string]any{
		"optional": nil,
	})

	assert.Empty(t, result)
}

// --- formatPositionalArgs tests ---

func TestFormatPositionalArgs_ValidSlice(t *testing.T) {
	t.Parallel()

	result := toolgen.FormatPositionalArgs([]any{"arg1", "arg2", 42})

	require.Len(t, result, 3)
	assert.Equal(t, "arg1", result[0])
	assert.Equal(t, "arg2", result[1])
	assert.Equal(t, "42", result[2])
}

func TestFormatPositionalArgs_EmptySlice(t *testing.T) {
	t.Parallel()

	result := toolgen.FormatPositionalArgs([]any{})

	assert.Empty(t, result)
}

func TestFormatPositionalArgs_NotSlice(t *testing.T) {
	t.Parallel()

	result := toolgen.FormatPositionalArgs("not-a-slice")

	assert.Nil(t, result)
}

func TestFormatPositionalArgs_Nil(t *testing.T) {
	t.Parallel()

	result := toolgen.FormatPositionalArgs(nil)

	assert.Nil(t, result)
}

// --- ToCopilotTools permission tests ---

func TestToCopilotTools_RequiresPermission(t *testing.T) {
	t.Parallel()

	tools := []toolgen.ToolDefinition{
		{
			Name:               "cluster_delete",
			Description:        "Delete a cluster",
			CommandPath:        "ksail cluster delete",
			CommandParts:       []string{"ksail", "cluster", "delete"},
			RequiresPermission: true,
		},
	}

	result := toolgen.ToCopilotTools(tools, toolgen.ToolOptions{})

	require.Len(t, result, 1)
	assert.False(t, result[0].SkipPermission, "write tools should NOT skip permission")
}

func TestToCopilotTools_ReadOnlySkipsPermission(t *testing.T) {
	t.Parallel()

	tools := []toolgen.ToolDefinition{
		{
			Name:               "cluster_list",
			Description:        "List clusters",
			CommandPath:        "ksail cluster list",
			CommandParts:       []string{"ksail", "cluster", "list"},
			RequiresPermission: false,
		},
	}

	result := toolgen.ToCopilotTools(tools, toolgen.ToolOptions{})

	require.Len(t, result, 1)
	assert.True(t, result[0].SkipPermission, "read-only tools should skip permission")
}

func TestToCopilotTools_HandlerInvocable(t *testing.T) {
	t.Parallel()

	tools := []toolgen.ToolDefinition{
		{
			Name:         "nonexistent_tool",
			Description:  "A nonexistent tool",
			CommandPath:  "nonexistent_binary_xyz_12345 tool",
			CommandParts: []string{"nonexistent_binary_xyz_12345", "tool"},
		},
	}

	result := toolgen.ToCopilotTools(tools, toolgen.ToolOptions{})

	require.Len(t, result, 1)
	require.NotNil(t, result[0].Handler)

	// Invoking the handler should not panic even if the binary doesn't exist;
	// it should return a result (with error details) and a nil Go error.
	invocation := copilot.ToolInvocation{
		Arguments: map[string]any{},
	}

	toolResult, err := result[0].Handler(invocation)

	require.NoError(t, err, "handler always returns nil Go error")
	assert.Equal(
		t,
		"failure",
		toolResult.ResultType,
		"command should fail since binary does not exist",
	)
}

func TestToCopilotTools_HandlerWithSessionLog(t *testing.T) {
	t.Parallel()

	sessionLog := toolgen.NewSessionLogRef()

	var logMessages []string

	sessionLog.Set(func(_ context.Context, message, _ string) {
		logMessages = append(logMessages, message)
	})

	tools := []toolgen.ToolDefinition{
		{
			Name:         "nonexistent_tool",
			Description:  "A nonexistent tool",
			CommandPath:  "nonexistent_binary_xyz_12345 tool",
			CommandParts: []string{"nonexistent_binary_xyz_12345", "tool"},
		},
	}

	opts := toolgen.ToolOptions{
		SessionLog: sessionLog,
	}

	result := toolgen.ToCopilotTools(tools, opts)
	invocation := copilot.ToolInvocation{
		Arguments: map[string]any{},
	}

	_, err := result[0].Handler(invocation)
	require.NoError(t, err)

	// Should have logged both start and completion messages
	require.Len(t, logMessages, 2)
	assert.Contains(t, logMessages[0], "Running:")
}

func TestToCopilotTools_HandlerWithNonMapArguments(t *testing.T) {
	t.Parallel()

	tools := []toolgen.ToolDefinition{
		{
			Name:         "echo_test",
			Description:  "An echo tool",
			CommandPath:  "echo test",
			CommandParts: []string{"echo", "test"},
		},
	}

	result := toolgen.ToCopilotTools(tools, toolgen.ToolOptions{})

	// Call with non-map arguments (e.g. nil)
	invocation := copilot.ToolInvocation{
		Arguments: nil,
	}

	toolResult, err := result[0].Handler(invocation)

	require.NoError(t, err)
	assert.NotEmpty(t, toolResult.TextResultForLLM)
}
