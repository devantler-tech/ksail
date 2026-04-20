package chat_test

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/devantler-tech/ksail/v7/pkg/cli/ui/chat"
)

// TestViewport_AssistantMessageWithSuccessTool tests rendering an assistant message with a success tool.
func TestViewport_AssistantMessageWithSuccessTool(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())

	successTool := chat.ExportNewToolExecutionFull(
		"read_file", chat.ToolStatusComplete, false, "cat /tmp/test", "file contents here",
	)

	chat.ExportSetMessages(model, []chat.MessageForTest{
		chat.ExportNewAssistantMessageWithTools(
			"Here is the file content:",
			[]*chat.ToolExecutionForTest{successTool},
		),
	})

	var updatedModel tea.Model = model

	updatedModel, _ = updatedModel.Update(tea.WindowSizeMsg{Width: 100, Height: 50})

	output := updatedModel.View()

	if !strings.Contains(output, "✓") {
		t.Error("expected success checkmark in rendered tool")
	}

	if !strings.Contains(output, "cat /tmp/test") {
		t.Error("expected tool command in rendered output")
	}
}

// TestViewport_AssistantMessageWithFailedTool tests rendering an assistant message with a failed tool.
func TestViewport_AssistantMessageWithFailedTool(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())

	failedTool := chat.ExportNewToolExecutionFull(
		"bash", chat.ToolStatusFailed, false, "invalid_cmd", "command not found",
	)

	chat.ExportSetMessages(model, []chat.MessageForTest{
		chat.ExportNewAssistantMessageWithTools(
			"I tried to run:",
			[]*chat.ToolExecutionForTest{failedTool},
		),
	})

	var updatedModel tea.Model = model

	updatedModel, _ = updatedModel.Update(tea.WindowSizeMsg{Width: 100, Height: 50})

	output := updatedModel.View()

	if !strings.Contains(output, "✗") {
		t.Error("expected failure mark in rendered tool")
	}
}

// TestViewport_AssistantMessageWithRunningTool tests rendering a running tool.
func TestViewport_AssistantMessageWithRunningTool(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())

	runningTool := chat.ExportNewToolExecutionFull(
		"bash", chat.ToolStatusRunning, false, "npm install", "installing...",
	)

	chat.ExportSetStreaming(model, true)
	chat.ExportSetTools(
		model,
		map[string]*chat.ToolExecutionForTest{"t-1": runningTool},
		[]string{"t-1"},
	)
	chat.ExportSetMessages(model, []chat.MessageForTest{
		chat.ExportNewStreamingAssistantMessage("Running command:"),
	})

	var updatedModel tea.Model = model

	updatedModel, _ = updatedModel.Update(tea.WindowSizeMsg{Width: 100, Height: 50})

	output := updatedModel.View()

	if !strings.Contains(output, "npm install") {
		t.Error("expected running tool command in output")
	}
}

// TestViewport_SuccessToolExpanded tests an expanded successful tool shows full output.
func TestViewport_SuccessToolExpanded(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())

	expandedTool := chat.ExportNewToolExecutionFull(
		"read_file", chat.ToolStatusComplete, true, "cat /tmp/test", "line 1\nline 2\nline 3",
	)

	chat.ExportSetMessages(model, []chat.MessageForTest{
		chat.ExportNewAssistantMessageWithTools(
			"Output:",
			[]*chat.ToolExecutionForTest{expandedTool},
		),
	})

	var updatedModel tea.Model = model

	updatedModel, _ = updatedModel.Update(tea.WindowSizeMsg{Width: 100, Height: 50})

	output := updatedModel.View()

	// Expanded tool should show full output
	if !strings.Contains(output, "line 1") || !strings.Contains(output, "line 3") {
		t.Error("expected full tool output when expanded")
	}
}

// TestViewport_FailedToolExpanded tests an expanded failed tool shows full error output.
func TestViewport_FailedToolExpanded(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())

	failedTool := chat.ExportNewToolExecutionFull(
		"bash", chat.ToolStatusFailed, true,
		"rm -rf /root", "Error: permission denied\nDetails: access forbidden",
	)

	chat.ExportSetMessages(model, []chat.MessageForTest{
		chat.ExportNewAssistantMessageWithTools(
			"Failed:",
			[]*chat.ToolExecutionForTest{failedTool},
		),
	})

	var updatedModel tea.Model = model

	updatedModel, _ = updatedModel.Update(tea.WindowSizeMsg{Width: 100, Height: 50})

	output := updatedModel.View()

	// Expanded failed tool should show full error
	if !strings.Contains(output, "permission denied") {
		t.Error("expected full error output when expanded failed tool")
	}
}

// TestViewport_UserMessageRendered tests that user messages render properly.
func TestViewport_UserMessageRendered(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetMessages(model, []chat.MessageForTest{
		chat.ExportNewUserMessage("What is Kubernetes?"),
	})

	var updatedModel tea.Model = model

	updatedModel, _ = updatedModel.Update(tea.WindowSizeMsg{Width: 100, Height: 50})

	output := updatedModel.View()

	if !strings.Contains(output, "What is Kubernetes?") {
		t.Error("expected user message content in viewport")
	}

	if !strings.Contains(output, "You") {
		t.Error("expected 'You' label for user message")
	}
}

// TestViewport_LegacyToolOutput tests legacy tool-output message rendering.
func TestViewport_LegacyToolOutput(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetMessages(model, []chat.MessageForTest{
		chat.ExportNewToolOutputMessage("legacy tool output content"),
	})

	var updatedModel tea.Model = model

	updatedModel, _ = updatedModel.Update(tea.WindowSizeMsg{Width: 100, Height: 50})

	output := updatedModel.View()

	if !strings.Contains(output, "legacy tool output") {
		t.Error("expected legacy tool output in viewport")
	}
}

// TestViewport_StreamingAssistantShowsCursor tests that streaming messages show cursor.
func TestViewport_StreamingAssistantShowsCursor(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetStreaming(model, true)
	chat.ExportSetMessages(model, []chat.MessageForTest{
		chat.ExportNewStreamingAssistantMessage("typing..."),
	})

	var updatedModel tea.Model = model

	updatedModel, _ = updatedModel.Update(tea.WindowSizeMsg{Width: 100, Height: 50})

	output := updatedModel.View()

	if !strings.Contains(output, "▌") {
		t.Error("expected cursor character in streaming assistant message")
	}
}

// TestViewport_MultipleToolsRendered tests rendering multiple interleaved tools.
func TestViewport_MultipleToolsRendered(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())

	tool1 := chat.ExportNewToolExecutionFull(
		"read_file", chat.ToolStatusComplete, false, "cat /tmp/a.txt", "contents of a",
	)

	tool2 := chat.ExportNewToolExecutionFull(
		"bash", chat.ToolStatusComplete, false, "echo hello", "hello",
	)

	chat.ExportSetMessages(model, []chat.MessageForTest{
		chat.ExportNewAssistantMessageWithTools(
			"Running two tools:",
			[]*chat.ToolExecutionForTest{tool1, tool2},
		),
	})

	var updatedModel tea.Model = model

	updatedModel, _ = updatedModel.Update(tea.WindowSizeMsg{Width: 100, Height: 50})

	output := updatedModel.View()

	// Both tools should have checkmarks
	checkmarkCount := strings.Count(output, "✓")
	if checkmarkCount < 2 {
		t.Errorf("expected at least 2 checkmarks for completed tools, got %d", checkmarkCount)
	}
}

// TestCommitToolsToLastAssistantMessage tests that tools are committed to the message.
func TestCommitToolsToLastAssistantMessage(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())

	// Set up tools in the model
	tool := chat.ExportNewToolExecutionFull("bash", chat.ToolStatusComplete, false, "echo hi", "hi")
	chat.ExportSetTools(
		model,
		map[string]*chat.ToolExecutionForTest{"t-1": tool},
		[]string{"t-1"},
	)

	// Add an assistant message
	chat.ExportSetMessages(model, []chat.MessageForTest{
		chat.ExportNewAssistantMessageWithRole("response with tools"),
	})

	// Commit tools
	chat.ExportCommitToolsToLastAssistantMessage(model)

	// Verify the message now has tools
	msgs := chat.ExportGetMessages(model)
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}

	toolCount := chat.ExportGetMessageToolCount(model, 0)
	if toolCount != 1 {
		t.Errorf("expected 1 tool committed to message, got %d", toolCount)
	}
}

// TestViewport_SuccessToolCollapsedShowsSummary tests collapsed tool shows summary.
func TestViewport_SuccessToolCollapsedShowsSummary(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())

	// Short output — should show as summary
	tool := chat.ExportNewToolExecutionFull(
		"bash", chat.ToolStatusComplete, false, "echo hello", "hello",
	)

	chat.ExportSetMessages(model, []chat.MessageForTest{
		chat.ExportNewAssistantMessageWithTools("", []*chat.ToolExecutionForTest{tool}),
	})

	var updatedModel tea.Model = model

	updatedModel, _ = updatedModel.Update(tea.WindowSizeMsg{Width: 100, Height: 50})

	output := updatedModel.View()

	if !strings.Contains(output, "hello") {
		t.Error("expected tool output summary in collapsed view")
	}
}

// TestViewport_ToolLongOutputSummary tests that long tool output shows truncated first line.
func TestViewport_ToolLongOutputSummary(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())

	longOutput := "This is a very long first line that exceeds the truncation limit for tool output summaries\n" +
		"second line\nthird line"
	tool := chat.ExportNewToolExecutionFull(
		"bash", chat.ToolStatusComplete, false, "long cmd", longOutput,
	)

	chat.ExportSetMessages(model, []chat.MessageForTest{
		chat.ExportNewAssistantMessageWithTools("", []*chat.ToolExecutionForTest{tool}),
	})

	var updatedModel tea.Model = model

	updatedModel, _ = updatedModel.Update(tea.WindowSizeMsg{Width: 200, Height: 50})

	output := updatedModel.View()

	// The collapsed view should show a summary — either the first part of the output
	// or the "+N lines" indicator
	if !strings.Contains(output, "✓") {
		t.Error("expected success checkmark for completed tool")
	}

	if !strings.Contains(output, "long cmd") {
		t.Error("expected tool command in output")
	}
}

// TestViewport_FailedToolCollapsedShowsSummary tests collapsed failed tool shows error summary.
func TestViewport_FailedToolCollapsedShowsSummary(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())

	tool := chat.ExportNewToolExecutionFull(
		"bash", chat.ToolStatusFailed, false, "bad-cmd", "error: not found",
	)

	chat.ExportSetMessages(model, []chat.MessageForTest{
		chat.ExportNewAssistantMessageWithTools("", []*chat.ToolExecutionForTest{tool}),
	})

	var updatedModel tea.Model = model

	updatedModel, _ = updatedModel.Update(tea.WindowSizeMsg{Width: 100, Height: 50})

	output := updatedModel.View()

	if !strings.Contains(output, "error: not found") {
		t.Error("expected error summary in collapsed failed tool")
	}
}

// TestViewport_ToolWithTextPosition tests tool rendering at a specific text position.
func TestViewport_ToolWithTextPosition(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())

	tool := chat.ExportNewToolExecutionWithPosition(
		"bash", chat.ToolStatusComplete, false, "echo hi", "hi", 10,
	)

	chat.ExportSetMessages(model, []chat.MessageForTest{
		chat.ExportNewAssistantMessageWithTools(
			"Before tool, after tool text",
			[]*chat.ToolExecutionForTest{tool},
		),
	})

	var updatedModel tea.Model = model

	updatedModel, _ = updatedModel.Update(tea.WindowSizeMsg{Width: 100, Height: 50})

	output := updatedModel.View()

	// Both the text before and after the tool should render
	if !strings.Contains(output, "Before") {
		t.Error("expected text before tool position")
	}

	if !strings.Contains(output, "after") {
		t.Error("expected text after tool position")
	}
}

// TestViewport_ToolWithEmptyCommand tests tool rendering without a command (uses name).
func TestViewport_ToolWithEmptyCommand(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())

	tool := chat.ExportNewToolExecutionFull(
		"custom_tool", chat.ToolStatusComplete, false, "", "output",
	)

	chat.ExportSetMessages(model, []chat.MessageForTest{
		chat.ExportNewAssistantMessageWithTools("", []*chat.ToolExecutionForTest{tool}),
	})

	var updatedModel tea.Model = model

	updatedModel, _ = updatedModel.Update(tea.WindowSizeMsg{Width: 100, Height: 50})

	output := updatedModel.View()

	// Should show humanized tool name instead of command
	if !strings.Contains(output, "Custom Tool") {
		t.Error("expected humanized tool name when no command")
	}
}

// TestCopyOutput_WithAssistantMessage tests Ctrl+R copy with an assistant message.
func TestCopyOutput_WithAssistantMessage(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetMessages(model, []chat.MessageForTest{
		chat.ExportNewAssistantMessageWithRole("Copy this text"),
	})

	var updatedModel tea.Model = model

	// Ctrl+R should attempt to copy
	updatedModel, _ = updatedModel.Update(tea.KeyMsg{Type: tea.KeyCtrlR})

	output := updatedModel.View()

	// Should show "Copied" feedback (clipboard may fail silently in CI)
	if !strings.Contains(output, "Copied") {
		t.Error("expected 'Copied' feedback after Ctrl+R with assistant message")
	}
}

// TestCopyOutput_NoAssistantMessage tests Ctrl+R without any assistant message.
func TestCopyOutput_NoAssistantMessage(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	// Only user messages, no assistant messages
	chat.ExportSetMessages(model, []chat.MessageForTest{
		chat.ExportNewUserMessage("hello"),
	})

	var updatedModel tea.Model = model

	updatedModel, _ = updatedModel.Update(tea.KeyMsg{Type: tea.KeyCtrlR})

	output := updatedModel.View()

	// No "Copied" feedback when there's no assistant message
	if strings.Contains(output, "Copied") {
		t.Error("expected no 'Copied' feedback without assistant messages")
	}
}

// TestCopyOutput_IgnoredWhileStreaming tests Ctrl+R is ignored during streaming.
func TestCopyOutput_IgnoredWhileStreaming(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetStreaming(model, true)
	chat.ExportSetMessages(model, []chat.MessageForTest{
		chat.ExportNewAssistantMessageWithRole("Some response"),
	})

	var updatedModel tea.Model = model

	updatedModel, _ = updatedModel.Update(tea.KeyMsg{Type: tea.KeyCtrlR})

	output := updatedModel.View()

	if strings.Contains(output, "Copied") {
		t.Error("expected no copy feedback while streaming")
	}
}

// TestToggleAllTools_WithCommittedMessageTools tests toggle with tools in committed messages.
func TestToggleAllTools_WithCommittedMessageTools(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())

	// Add tools both in the model and in committed messages
	tool := chat.ExportNewToolExecutionFull(
		"bash", chat.ToolStatusComplete, true, "echo hi", "hi",
	)

	chat.ExportSetMessages(model, []chat.MessageForTest{
		chat.ExportNewAssistantMessageWithTools("", []*chat.ToolExecutionForTest{tool}),
	})

	var updatedModel tea.Model = model

	// Toggle all tools — should collapse since first tool is expanded
	updatedModel, _ = updatedModel.Update(tea.KeyMsg{Type: tea.KeyCtrlT})

	// Toggle again — should expand
	updatedModel, _ = updatedModel.Update(tea.KeyMsg{Type: tea.KeyCtrlT})

	output := updatedModel.View()
	if output == "" {
		t.Error("expected non-empty view after toggling tools")
	}
}

// TestViewport_ToolWithEmptyOutput tests tool rendering with empty output.
func TestViewport_ToolWithEmptyOutput(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())

	tool := chat.ExportNewToolExecutionFull(
		"bash", chat.ToolStatusComplete, false, "echo", "",
	)

	chat.ExportSetMessages(model, []chat.MessageForTest{
		chat.ExportNewAssistantMessageWithTools("", []*chat.ToolExecutionForTest{tool}),
	})

	var updatedModel tea.Model = model

	updatedModel, _ = updatedModel.Update(tea.WindowSizeMsg{Width: 100, Height: 50})

	output := updatedModel.View()

	// Tool with no output should still render the checkmark
	if !strings.Contains(output, "✓") {
		t.Error("expected success checkmark even with empty output")
	}
}

// TestViewport_RunningToolWithoutOutput tests running tool rendering without output.
func TestViewport_RunningToolWithoutOutput(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())

	runningTool := chat.ExportNewToolExecutionFull(
		"bash", chat.ToolStatusRunning, false, "long task", "",
	)

	chat.ExportSetStreaming(model, true)
	chat.ExportSetTools(
		model,
		map[string]*chat.ToolExecutionForTest{"t-1": runningTool},
		[]string{"t-1"},
	)
	chat.ExportSetMessages(model, []chat.MessageForTest{
		chat.ExportNewStreamingAssistantMessage("Running:"),
	})

	var updatedModel tea.Model = model

	updatedModel, _ = updatedModel.Update(tea.WindowSizeMsg{Width: 100, Height: 50})

	output := updatedModel.View()

	if !strings.Contains(output, "long task") {
		t.Error("expected running tool command even without output")
	}
}
