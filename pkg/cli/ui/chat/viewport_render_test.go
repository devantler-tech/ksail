package chat_test

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/devantler-tech/ksail/v7/pkg/cli/ui/chat"
	"github.com/stretchr/testify/assert"
)

// --- renderMessage tests ---

func TestRenderMessage_UserMessage(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetMessages(model, []chat.MessageForTest{
		chat.ExportNewUserMessage("Hello world"),
	})

	// Trigger a render by doing a window resize
	var updated tea.Model = model

	updated, _ = updated.Update(tea.WindowSizeMsg{Width: 100, Height: 40})

	output := updated.View()

	assert.Contains(t, output, "You")
	assert.Contains(t, output, "Hello world")
}

func TestRenderMessage_AssistantMessageWithContent(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetMessages(model, []chat.MessageForTest{
		chat.ExportNewAssistantMessage("This is the response"),
	})

	var updated tea.Model = model

	updated, _ = updated.Update(tea.WindowSizeMsg{Width: 100, Height: 40})

	output := updated.View()

	assert.Contains(t, output, "KSail")
	assert.Contains(t, output, "This is the response")
}

func TestRenderMessage_StreamingAssistantShowsSpinner(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetStreaming(model, true)
	chat.ExportSetMessages(model, []chat.MessageForTest{
		chat.ExportNewStreamingAssistantMessage("partial text"),
	})

	var updated tea.Model = model

	updated, _ = updated.Update(tea.WindowSizeMsg{Width: 100, Height: 40})

	output := updated.View()

	assert.Contains(t, output, "partial text")
	// Streaming message should show cursor indicator
	assert.Contains(t, output, "▌")
}

func TestRenderMessage_ToolOutputMessage(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetMessages(model, []chat.MessageForTest{
		chat.ExportNewToolOutputMessage("tool output content"),
	})

	var updated tea.Model = model

	updated, _ = updated.Update(tea.WindowSizeMsg{Width: 100, Height: 40})

	output := updated.View()

	assert.Contains(t, output, "tool output content")
}

// --- renderToolInline tests ---

func TestRenderToolInline_RunningTool(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetStreaming(model, true)

	// Add a streaming message and a running tool
	chat.ExportSetMessages(model, []chat.MessageForTest{
		chat.ExportNewStreamingAssistantMessage("checking"),
	})

	var updated tea.Model = model

	updated, _ = updated.Update(
		chat.ExportNewToolStartMsg("t1", "ksail_cluster_list", "ksail cluster list"),
	)

	m := updated

	m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})

	output := m.View()

	assert.Contains(t, output, "ksail cluster list")
}

func TestRenderToolInline_SuccessTool_Expanded(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetStreaming(model, true)

	chat.ExportSetMessages(model, []chat.MessageForTest{
		chat.ExportNewStreamingAssistantMessage("done"),
	})

	var updated tea.Model = model

	updated, _ = updated.Update(chat.ExportNewToolStartMsg("t1", "bash", "> ls"))
	updated, _ = updated.Update(chat.ExportNewToolEndMsg("t1", "bash", "file1\nfile2", true))

	updated, _ = updated.Update(tea.WindowSizeMsg{Width: 100, Height: 40})

	output := updated.View()

	assert.Contains(t, output, "✓")
	assert.Contains(t, output, "ls")
	assert.Contains(t, output, "file1")
}

func TestRenderToolInline_FailedTool(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetStreaming(model, true)

	chat.ExportSetMessages(model, []chat.MessageForTest{
		chat.ExportNewStreamingAssistantMessage(""),
	})

	var updated tea.Model = model

	updated, _ = updated.Update(chat.ExportNewToolStartMsg("t1", "bash", "> rm -rf /"))
	updated, _ = updated.Update(chat.ExportNewToolEndMsg("t1", "bash", "permission denied", false))

	updated, _ = updated.Update(tea.WindowSizeMsg{Width: 100, Height: 40})

	output := updated.View()

	assert.Contains(t, output, "✗")
}

func TestRenderToolInline_CollapsedSuccessTool(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())

	// Use pre-built tool with collapsed state
	tools := map[string]*chat.ToolExecutionForTest{
		"t1": chat.ExportNewToolExecutionFull(
			"bash",
			chat.ToolStatusComplete,
			false,
			"> ls",
			"file1",
		),
	}
	chat.ExportSetTools(model, tools, []string{"t1"})

	chat.ExportSetMessages(model, []chat.MessageForTest{
		chat.ExportNewStreamingAssistantMessage("response"),
	})

	var updated tea.Model = model

	updated, _ = updated.Update(tea.WindowSizeMsg{Width: 100, Height: 40})

	output := updated.View()

	assert.Contains(t, output, "✓")
	// Collapsed tools should show summary
	assert.Contains(t, output, "file1")
}

// --- renderPendingPrompts tests ---

func TestRenderPendingPrompts_ShowsSteeringFirst(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetStreaming(model, true)

	// Queue a prompt first
	typed := typeText(model, "queued task")
	typed, _ = typed.Update(ctrlQKey())

	reassertStreaming(t, typed)

	// Then add a steering prompt
	typed = typeText(typed, "steering msg")
	typed, _ = typed.Update(tea.KeyMsg{Type: tea.KeyEnter})

	output := typed.View()

	// Steering should appear before queued
	steeringIdx := strings.Index(output, "STEERING")
	queuedIdx := strings.Index(output, "QUEUED")

	if steeringIdx >= 0 && queuedIdx >= 0 {
		assert.Less(t, steeringIdx, queuedIdx)
	}
}

// --- renderExitConfirmModal tests ---

func TestRenderExitConfirmModal_ShowsPendingCount(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetStreaming(model, true)

	// Queue a prompt
	typed := typeText(model, "pending task")
	typed, _ = typed.Update(ctrlQKey())

	// Stop streaming and trigger exit confirm
	modelState := requireModel(t, typed)
	chat.ExportSetStreaming(modelState, false)
	chat.ExportSetConfirmExit(modelState, true)

	output := modelState.View()

	assert.Contains(t, output, "pending prompt")
}

// --- renderFooter tests ---

func TestRenderFooter_ShowsQuotaInfo(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetLastQuotaSnapshots(model, map[string]chat.QuotaSnapshotForTest{
		"premium": chat.ExportNewQuotaSnapshot(500, 50, 90, false, "Jan 2"),
	})

	// Give enough width for both help and quota
	var updated tea.Model = model

	updated, _ = updated.Update(tea.WindowSizeMsg{Width: 200, Height: 40})

	output := updated.View()

	assert.Contains(t, output, "50/500 reqs")
	assert.Contains(t, output, "90%")
	assert.Contains(t, output, "resets Jan 2")
}

func TestRenderFooter_UnlimitedQuota(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetLastQuotaSnapshots(model, map[string]chat.QuotaSnapshotForTest{
		"premium": chat.ExportNewQuotaSnapshot(0, 0, 100, true, ""),
	})

	var updated tea.Model = model

	updated, _ = updated.Update(tea.WindowSizeMsg{Width: 200, Height: 40})

	output := updated.View()

	assert.Contains(t, output, "∞ reqs")
}

// --- buildModelStatusText tests ---

func TestBuildModelStatusText_AutoModeWithMultiplier(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		model  string
		config string
		models []struct {
			id   string
			mult float64
		}
		contains []string
	}{
		{
			name:     "auto mode unresolved",
			model:    "",
			config:   "",
			contains: []string{"auto"},
		},
		{
			name:     "explicit model",
			model:    "gpt-4o",
			config:   "gpt-4o",
			contains: []string{"gpt-4o"},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			model := chat.NewModel(newTestParams())
			chat.ExportSetCurrentModel(model, testCase.model)
			chat.ExportSetSessionConfigModel(model, testCase.config)

			result := chat.ExportBuildModelStatusText(model)
			for _, s := range testCase.contains {
				assert.Contains(t, result, s)
			}
		})
	}
}

// --- buildReasoningEffortStatusText tests ---

func TestBuildReasoningEffortStatusText_ShowsEffort(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetSessionConfigReasoningEffort(model, "high")

	result := chat.ExportBuildStatusText(model)

	assert.Contains(t, result, "high effort")
}

func TestBuildReasoningEffortStatusText_EmptyWhenNoEffort(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetSessionConfigReasoningEffort(model, "")

	result := chat.ExportBuildStatusText(model)

	assert.NotContains(t, result, "effort")
}

// --- formatMultiplier tests (rendering context) ---

func TestFormatMultiplier_RenderCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		mult     float64
		expected string
	}{
		{name: "integer value", mult: 1.0, expected: "1"},
		{name: "one decimal", mult: 1.5, expected: "1.5"},
		{name: "two decimals", mult: 1.25, expected: "1.25"},
		{name: "trailing zeros removed", mult: 2.10, expected: "2.1"},
		{name: "zero", mult: 0, expected: "0"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := chat.ExportFormatMultiplier(testCase.mult)
			assert.Equal(t, testCase.expected, result)
		})
	}
}

// --- renderInputOrModal tests ---

func TestRenderInputOrModal_ShowsHelpOverlay(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetShowHelpOverlay(model, true)

	var updated tea.Model = model

	updated, _ = updated.Update(tea.WindowSizeMsg{Width: 120, Height: 50})

	output := updated.View()

	// When help overlay is shown, it should contain keybinding info
	assert.Contains(t, output, "send")
}

func TestRenderInputOrModal_ShowsPermissionModal(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	resp := make(chan bool, 1)
	chat.ExportSetPendingPermission(model, "bash", "rm -rf /tmp/test", "", resp)

	var updated tea.Model = model

	updated, _ = updated.Update(tea.WindowSizeMsg{Width: 100, Height: 40})

	output := updated.View()

	assert.Contains(t, output, "rm -rf /tmp/test")
}

// --- truncateString tests (viewport context) ---

//nolint:gosmopolitan // This test intentionally exercises Han-script truncation.
func TestTruncateString_ViewportCases(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		maxLen   int
		expected string
	}{
		{name: "short string", input: "hello", maxLen: 10, expected: "hello"},
		{name: "exact length", input: "hello", maxLen: 5, expected: "hello"},
		{name: "truncated", input: "hello world", maxLen: 5, expected: "he..."},
		{name: "empty string", input: "", maxLen: 5, expected: ""},
		{name: "unicode safe", input: "日本語テスト", maxLen: 6, expected: "日本語テスト"},
		{name: "unicode truncated", input: "日本語テスト", maxLen: 5, expected: "日本..."},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := chat.ExportTruncateString(testCase.input, testCase.maxLen)
			assert.Equal(t, testCase.expected, result)
		})
	}
}

// --- addToPromptHistory tests ---

func TestAddToPromptHistory_AddsNewPrompt(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())

	chat.ExportAddToPromptHistory(model, "first")
	chat.ExportAddToPromptHistory(model, "second")

	history := chat.ExportGetHistory(model)
	assert.Len(t, history, 2)
	assert.Equal(t, "first", history[0])
	assert.Equal(t, "second", history[1])
}

func TestAddToPromptHistory_SkipsDuplicateConsecutive(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())

	chat.ExportAddToPromptHistory(model, "same")
	chat.ExportAddToPromptHistory(model, "same")
	chat.ExportAddToPromptHistory(model, "different")
	chat.ExportAddToPromptHistory(model, "different")

	history := chat.ExportGetHistory(model)
	assert.Len(t, history, 2)
}

func TestAddToPromptHistory_SkipsEmpty(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())

	chat.ExportAddToPromptHistory(model, "")

	history := chat.ExportGetHistory(model)
	assert.Empty(t, history)
}

func TestAddToPromptHistory_ResetsHistoryIndex(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetHistory(model, []string{"previous"})

	chat.ExportAddToPromptHistory(model, "new")

	assert.Equal(t, -1, chat.ExportGetHistoryIndex(model))
}
