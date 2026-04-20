package toolgen_test

import (
	"context"
	"log/slog"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/toolgen"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

//nolint:goconst // Repeated literals keep the test cases explicit.
func TestExecuteTool_SimpleCommand(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("echo is a shell builtin on Windows")
	}

	tool := toolgen.ToolDefinition{
		Name:         "hello",
		CommandPath:  "echo hello",
		CommandParts: []string{"echo", "hello"},
	}

	opts := toolgen.ToolOptions{}
	output, err := toolgen.ExecuteTool(context.Background(), tool, map[string]any{}, opts)

	require.NoError(t, err)
	assert.Contains(t, output, "hello")
}

func TestExecuteTool_WithLogger(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("echo is a shell builtin on Windows")
	}

	tool := toolgen.ToolDefinition{
		Name:         "hello",
		CommandPath:  "echo hello",
		CommandParts: []string{"echo", "hello"},
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))

	opts := toolgen.ToolOptions{
		Logger: logger,
	}
	output, err := toolgen.ExecuteTool(context.Background(), tool, map[string]any{}, opts)

	require.NoError(t, err)
	assert.Contains(t, output, "hello")
}

func TestExecuteTool_FailureWithLogger(t *testing.T) {
	t.Parallel()

	tool := toolgen.ToolDefinition{
		Name:         "bad_cmd",
		CommandPath:  "nonexistent_binary_xyz_12345 fail",
		CommandParts: []string{"nonexistent_binary_xyz_12345", "fail"},
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))

	opts := toolgen.ToolOptions{
		Logger: logger,
	}
	_, err := toolgen.ExecuteTool(context.Background(), tool, map[string]any{}, opts)

	require.Error(t, err)
}

func TestExecuteTool_WithTimeout(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("echo is a shell builtin on Windows")
	}

	tool := toolgen.ToolDefinition{
		Name:         "hello",
		CommandPath:  "echo hello",
		CommandParts: []string{"echo", "hello"},
	}

	opts := toolgen.ToolOptions{
		CommandTimeout: 10 * time.Second,
	}
	output, err := toolgen.ExecuteTool(context.Background(), tool, map[string]any{}, opts)

	require.NoError(t, err)
	assert.Contains(t, output, "hello")
}

func TestExecuteTool_ZeroTimeout(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("echo is a shell builtin on Windows")
	}

	tool := toolgen.ToolDefinition{
		Name:         "hello",
		CommandPath:  "echo hello",
		CommandParts: []string{"echo", "hello"},
	}

	opts := toolgen.ToolOptions{
		CommandTimeout: 0, // No timeout
	}
	output, err := toolgen.ExecuteTool(context.Background(), tool, map[string]any{}, opts)

	require.NoError(t, err)
	assert.Contains(t, output, "hello")
}

func TestExecuteTool_WithStreaming(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("echo is a shell builtin on Windows")
	}

	outputChan := make(chan toolgen.OutputChunk, 10)

	tool := toolgen.ToolDefinition{
		Name:         "hello",
		CommandPath:  "echo hello",
		CommandParts: []string{"echo", "hello"},
	}

	opts := toolgen.ToolOptions{
		OutputChan: outputChan,
	}

	output, err := toolgen.ExecuteTool(context.Background(), tool, map[string]any{}, opts)

	require.NoError(t, err)
	assert.Contains(t, output, "hello")

	// Drain the channel and verify chunks
	close(outputChan)

	var chunks []toolgen.OutputChunk
	for chunk := range outputChan {
		chunks = append(chunks, chunk)
	}

	require.NotEmpty(t, chunks, "should have received at least one chunk")
	assert.Equal(t, "hello", chunks[0].ToolID)
	assert.Equal(t, "stdout", chunks[0].Source)
	assert.Contains(t, chunks[0].Chunk, "hello")
}

func TestExecuteTool_StreamingFailure(t *testing.T) {
	t.Parallel()

	outputChan := make(chan toolgen.OutputChunk, 10)

	tool := toolgen.ToolDefinition{
		Name:         "fail_cmd",
		CommandPath:  "false",
		CommandParts: []string{"false"},
	}

	opts := toolgen.ToolOptions{
		OutputChan: outputChan,
	}

	_, err := toolgen.ExecuteTool(context.Background(), tool, map[string]any{}, opts)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "command failed")

	close(outputChan)
}

func TestExecuteTool_StreamingMultilineOutput(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("printf behavior differs on Windows")
	}

	outputChan := make(chan toolgen.OutputChunk, 20)

	// Use printf to generate multi-line output
	tool := toolgen.ToolDefinition{
		Name:         "multiline",
		CommandPath:  "printf line1\\nline2\\nline3\\n",
		CommandParts: []string{"printf", "line1\\nline2\\nline3\\n"},
	}

	opts := toolgen.ToolOptions{
		OutputChan: outputChan,
	}

	output, err := toolgen.ExecuteTool(context.Background(), tool, map[string]any{}, opts)

	require.NoError(t, err)
	assert.Contains(t, output, "line1")
	assert.Contains(t, output, "line2")
	assert.Contains(t, output, "line3")

	close(outputChan)

	var chunks []toolgen.OutputChunk
	for chunk := range outputChan {
		chunks = append(chunks, chunk)
	}

	assert.GreaterOrEqual(t, len(chunks), 3, "should have at least 3 chunks for 3 lines")
}

func TestExecuteTool_WithConsolidatedTool(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("echo is a shell builtin on Windows")
	}

	tool := toolgen.ToolDefinition{
		Name:            "echo_tools",
		CommandPath:     "echo tools",
		CommandParts:    []string{"echo", "tools"},
		IsConsolidated:  true,
		SubcommandParam: "action",
		Subcommands: map[string]*toolgen.SubcommandDef{
			"hello": {
				Name:         "hello",
				Description:  "Print hello",
				CommandParts: []string{"echo", "hello"},
				AcceptsArgs:  false,
				Flags:        map[string]*toolgen.FlagDef{},
			},
		},
	}

	params := map[string]any{
		"action": "hello",
	}

	opts := toolgen.ToolOptions{}
	output, err := toolgen.ExecuteTool(context.Background(), tool, params, opts)

	require.NoError(t, err)
	assert.Contains(t, output, "hello")
}

func TestExecuteTool_BuildCommandArgsFails(t *testing.T) {
	t.Parallel()

	// Consolidated tool with missing subcommand param triggers BuildCommandArgs error
	tool := toolgen.ToolDefinition{
		Name:            "bad_tool",
		CommandPath:     "test bad",
		CommandParts:    []string{"test", "bad"},
		IsConsolidated:  true,
		SubcommandParam: "action",
		Subcommands:     map[string]*toolgen.SubcommandDef{},
	}

	params := map[string]any{
		// Missing "action" param
	}

	opts := toolgen.ToolOptions{}
	_, err := toolgen.ExecuteTool(context.Background(), tool, params, opts)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "building command args")
}
