package chat_test

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/devantler-tech/ksail/v7/pkg/cli/ui/chat"
)

// TestEscape_NotStreaming_ShowsExitConfirm tests that Esc sets confirmExit when not streaming.
func TestEscape_NotStreaming_ShowsExitConfirm(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())

	var updatedModel tea.Model = model

	updatedModel, _ = updatedModel.Update(tea.KeyMsg{Type: tea.KeyEsc})

	output := updatedModel.View()

	if !strings.Contains(output, "Exit") {
		t.Error("expected exit confirmation after pressing Esc when not streaming")
	}
}

// TestEscape_WhileStreaming_CancelsStream tests that Esc cancels streaming.
func TestEscape_WhileStreaming_CancelsStream(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetStreaming(model, true)
	// Add an assistant message that's being streamed
	chat.ExportSetMessages(model, []chat.MessageForTest{
		chat.ExportNewAssistantMessageWithRole("partial response"),
	})

	var updatedModel tea.Model = model

	updatedModel, _ = updatedModel.Update(tea.KeyMsg{Type: tea.KeyEsc})

	// Should NOT show exit confirmation (stream cancelled instead)
	output := updatedModel.View()

	if strings.Contains(output, "Exit KSail") {
		t.Error("expected stream cancellation, not exit confirmation")
	}
}

// TestExitConfirm_YExits tests that pressing Y on exit confirm quits.
func TestExitConfirm_YExits(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetConfirmExit(model, true)

	var updatedModel tea.Model = model

	updatedModel, _ = updatedModel.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})

	chatModel, ok := updatedModel.(*chat.Model)
	if !ok {
		t.Fatal("expected *chat.Model type assertion to succeed")
	}

	if !chat.ExportGetQuitting(chatModel) {
		t.Error("expected quitting to be true after pressing Y on exit confirm")
	}
}

// TestExitConfirm_NCloses tests that pressing N dismisses exit confirmation.
func TestExitConfirm_NCloses(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetConfirmExit(model, true)

	var updatedModel tea.Model = model

	updatedModel, _ = updatedModel.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})

	chatModel, ok := updatedModel.(*chat.Model)
	if !ok {
		t.Fatal("expected *chat.Model type assertion to succeed")
	}

	if chat.ExportGetConfirmExit(chatModel) {
		t.Error("expected confirmExit to be false after pressing N")
	}
}

// TestExitConfirm_EscCloses tests that pressing Esc dismisses exit confirmation.
func TestExitConfirm_EscCloses(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetConfirmExit(model, true)

	var updatedModel tea.Model = model

	updatedModel, _ = updatedModel.Update(tea.KeyMsg{Type: tea.KeyEsc})

	chatModel, ok := updatedModel.(*chat.Model)
	if !ok {
		t.Fatal("expected *chat.Model type assertion to succeed")
	}

	if chat.ExportGetConfirmExit(chatModel) {
		t.Error("expected confirmExit to be false after pressing Esc")
	}
}

// TestHistoryUp_NavigatesHistory tests up arrow navigates prompt history.
func TestHistoryUp_NavigatesHistory(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetHistory(model, []string{"first prompt", "second prompt"})

	var updatedModel tea.Model = model

	// Press up to go to last history entry
	updatedModel, _ = updatedModel.Update(tea.KeyMsg{Type: tea.KeyUp})

	chatModel, assertionOK := updatedModel.(*chat.Model)
	if !assertionOK {
		t.Fatal("expected *chat.Model type assertion to succeed")
	}

	value := chat.ExportGetTextareaValue(chatModel)
	if value != "second prompt" {
		t.Errorf("expected 'second prompt' in textarea, got %q", value)
	}

	// Press up again to go to first history entry
	updatedModel, _ = updatedModel.Update(tea.KeyMsg{Type: tea.KeyUp})

	chatModel, assertionOK = updatedModel.(*chat.Model)
	if !assertionOK {
		t.Fatal("expected *chat.Model type assertion to succeed")
	}

	value = chat.ExportGetTextareaValue(chatModel)
	if value != "first prompt" {
		t.Errorf("expected 'first prompt' in textarea, got %q", value)
	}
}

// TestHistoryDown_RestoresSavedInput tests down arrow returns to saved input.
func TestHistoryDown_RestoresSavedInput(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetHistory(model, []string{"old prompt"})

	var updatedModel tea.Model = model

	// Type something first
	updatedModel = typeText(updatedModel, "current input")

	// Go up (saves current input, shows history)
	updatedModel, _ = updatedModel.Update(tea.KeyMsg{Type: tea.KeyUp})

	// Go down (should restore saved input)
	updatedModel, _ = updatedModel.Update(tea.KeyMsg{Type: tea.KeyDown})

	chatModel, ok := updatedModel.(*chat.Model)
	if !ok {
		t.Fatal("expected *chat.Model type assertion to succeed")
	}

	value := chat.ExportGetTextareaValue(chatModel)
	if value != "current input" {
		t.Errorf("expected 'current input' restored, got %q", value)
	}
}

// TestHistoryUp_IgnoredWhileStreaming tests that history navigation is ignored during streaming.
func TestHistoryUp_IgnoredWhileStreaming(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetStreaming(model, true)
	chat.ExportSetHistory(model, []string{"old prompt"})

	var updatedModel tea.Model = model

	updatedModel, _ = updatedModel.Update(tea.KeyMsg{Type: tea.KeyUp})

	chatModel, ok := updatedModel.(*chat.Model)
	if !ok {
		t.Fatal("expected *chat.Model type assertion to succeed")
	}

	if chat.ExportGetHistoryIndex(chatModel) != -1 {
		t.Error("expected history index to remain -1 when streaming")
	}
}

// TestHistoryUp_EmptyHistory tests that up arrow does nothing with empty history.
func TestHistoryUp_EmptyHistory(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())

	var updatedModel tea.Model = model

	updatedModel, _ = updatedModel.Update(tea.KeyMsg{Type: tea.KeyUp})

	chatModel, ok := updatedModel.(*chat.Model)
	if !ok {
		t.Fatal("expected *chat.Model type assertion to succeed")
	}

	if chat.ExportGetHistoryIndex(chatModel) != -1 {
		t.Error("expected history index to remain -1 with empty history")
	}
}

// TestOpenReasoningPicker_CtrlE tests that Ctrl+E opens the reasoning picker.
func TestOpenReasoningPicker_CtrlE(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())

	var updatedModel tea.Model = model

	updatedModel, _ = updatedModel.Update(tea.KeyMsg{Type: tea.KeyCtrlE})

	output := updatedModel.View()

	if !strings.Contains(output, "Reasoning Effort") {
		t.Error("expected reasoning picker to open after Ctrl+E")
	}
}

// TestOpenReasoningPicker_IgnoredWhileStreaming tests that Ctrl+E is ignored during streaming.
func TestOpenReasoningPicker_IgnoredWhileStreaming(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetStreaming(model, true)

	var updatedModel tea.Model = model

	updatedModel, _ = updatedModel.Update(tea.KeyMsg{Type: tea.KeyCtrlE})

	output := updatedModel.View()

	if strings.Contains(output, "Reasoning Effort") {
		t.Error("expected reasoning picker to not open when streaming")
	}
}

// TestToggleAllTools_CollapsesExpandedTools tests Ctrl+T toggling tool expansion.
func TestToggleAllTools_CollapsesExpandedTools(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	// Add a completed, expanded tool
	tools := map[string]*chat.ToolExecutionForTest{
		"tool-1": chat.ExportNewToolExecution("test_tool", chat.ToolStatusComplete, true),
	}
	chat.ExportSetTools(model, tools, []string{"tool-1"})

	var updatedModel tea.Model = model

	// Toggle all tools (should collapse since first tool is expanded)
	updatedModel, _ = updatedModel.Update(tea.KeyMsg{Type: tea.KeyCtrlT})

	// Just verify no panic — the toggle itself works on internal state
	output := updatedModel.View()
	if output == "" {
		t.Error("expected non-empty view after toggling tools")
	}
}

// TestF1_OpensHelpOverlay tests that F1 opens the help overlay.
func TestF1_OpensHelpOverlay(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())

	var updatedModel tea.Model = model

	updatedModel, _ = updatedModel.Update(tea.KeyMsg{Type: tea.KeyF1})
	updatedModel, _ = updatedModel.Update(tea.WindowSizeMsg{Width: 120, Height: 50})

	output := updatedModel.View()

	if !strings.Contains(output, "send") {
		t.Error("expected help overlay to show after F1")
	}
}

// TestF1_ClosesHelpOverlay tests that pressing F1 again closes the overlay.
func TestF1_ClosesHelpOverlay(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetShowHelpOverlay(model, true)

	var updatedModel tea.Model = model

	// F1 should close it
	updatedModel, _ = updatedModel.Update(tea.KeyMsg{Type: tea.KeyF1})

	// The overlay should now be closed — keys other than F1/Esc are ignored in overlay
	chatModel, ok := updatedModel.(*chat.Model)
	if !ok {
		t.Fatal("expected *chat.Model type assertion to succeed")
	}

	// Help overlay should be hidden — verify no "sessions" + "newline" overlap
	output := chatModel.View()
	if strings.Contains(output, "sessions") && strings.Contains(output, "newline") {
		t.Error("expected help overlay to be closed after pressing F1")
	}
}

// TestPageUpDown_ScrollsViewport tests page up/down viewport scrolling.
func TestPageUpDown_ScrollsViewport(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())

	var updatedModel tea.Model = model

	// Set a large size
	updatedModel, _ = updatedModel.Update(tea.WindowSizeMsg{Width: 100, Height: 50})

	// Page up and down should not panic
	updatedModel, _ = updatedModel.Update(tea.KeyMsg{Type: tea.KeyPgUp})
	updatedModel, _ = updatedModel.Update(tea.KeyMsg{Type: tea.KeyPgDown})

	output := updatedModel.View()
	if output == "" {
		t.Error("expected non-empty view after page up/down")
	}
}

// TestModelUnavailableClear tests that modelUnavailableClearMsg clears the feedback.
func TestModelUnavailableClear(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetShowModelUnavailableFeedback(model, true)
	chat.ExportSetModelUnavailableReason(model, "rate limited")

	clearMsg := chat.ExportNewModelUnavailableClearMsg()

	var updatedModel tea.Model = model

	updatedModel, _ = updatedModel.Update(clearMsg)

	// The feedback should be cleared — verify via status text
	output := updatedModel.View()

	if strings.Contains(output, "rate limited") {
		t.Error("expected model unavailable reason to be cleared")
	}
}

// TestExitConfirmModal_ShowsPendingCount tests exit modal with pending prompts.
func TestExitConfirmModal_ShowsPendingCount(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetStreaming(model, true)

	// Queue a prompt
	updatedModel := typeText(model, "pending task")

	updatedModel, _ = updatedModel.Update(ctrlQKey())

	// Now set confirmExit
	chatModel, ok := updatedModel.(*chat.Model)
	if !ok {
		t.Fatal("expected *chat.Model type assertion to succeed")
	}

	chat.ExportSetConfirmExit(chatModel, true)
	chat.ExportSetStreaming(chatModel, false)

	output := chatModel.View()

	if !strings.Contains(output, "pending") {
		t.Error("expected pending prompt count in exit confirmation modal")
	}
}

// TestAltEnter_InsertsNewline tests that Alt+Enter inserts a newline.
func TestAltEnter_InsertsNewline(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())

	var updatedModel tea.Model = model

	updatedModel = typeText(updatedModel, "line1")
	updatedModel, _ = updatedModel.Update(tea.KeyMsg{Type: tea.KeyEnter, Alt: true})
	updatedModel = typeText(updatedModel, "line2")

	chatModel, ok := updatedModel.(*chat.Model)
	if !ok {
		t.Fatal("expected *chat.Model type assertion to succeed")
	}

	value := chat.ExportGetTextareaValue(chatModel)
	if !strings.Contains(value, "line1") || !strings.Contains(value, "line2") {
		t.Errorf("expected both lines in textarea, got %q", value)
	}
}
