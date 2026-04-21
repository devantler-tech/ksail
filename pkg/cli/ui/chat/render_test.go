package chat_test

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/devantler-tech/ksail/v7/pkg/cli/ui/chat"
	copilot "github.com/github/copilot-sdk/go"
)

// TestStatusBar_ModeDisplay tests that the status bar shows the current chat mode.
func TestStatusBar_ModeDisplay(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		mode     chat.ChatMode
		expected string
	}{
		{
			name:     "interactive mode in status",
			mode:     chat.InteractiveMode,
			expected: "</> interactive",
		},
		{name: "plan mode in status", mode: chat.PlanMode, expected: "\u2261 plan"},
		{name: "autopilot mode in status", mode: chat.AutopilotMode, expected: "⚡ autopilot"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			model := chat.NewModel(newTestParams())
			chat.ExportSetChatMode(model, testCase.mode)

			// Test buildStatusText directly to avoid false passes from footer help text
			// which also renders the mode label via the Tab keybinding description.
			statusText := chat.ExportBuildStatusText(model)

			if !strings.Contains(statusText, testCase.expected) {
				t.Errorf("expected %q in status text, got: %q", testCase.expected, statusText)
			}
		})
	}
}

// TestStatusBar_AutoModelUnresolved tests auto model display when not yet resolved.
func TestStatusBar_AutoModelUnresolved(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetSessionConfigModel(model, "")
	chat.ExportSetCurrentModel(model, "")

	output := model.View()

	if !strings.Contains(output, "auto") {
		t.Error("expected 'auto' in status bar for unresolved auto mode")
	}
}

// TestStatusBar_ExplicitModel tests that an explicit model name appears in the status bar.
func TestStatusBar_ExplicitModel(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetSessionConfigModel(model, "gpt-4o")
	chat.ExportSetCurrentModel(model, "gpt-4o")

	output := model.View()

	if !strings.Contains(output, "gpt-4o") {
		t.Error("expected 'gpt-4o' in status bar for explicit model")
	}
}

// TestStatusBar_AutoModelResolved tests auto model display with resolved model.
func TestStatusBar_AutoModelResolved(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetSessionConfigModel(model, "")
	chat.ExportSetCurrentModel(model, "gpt-4o")
	chat.ExportSetAvailableModels(model, []copilot.ModelInfo{
		{
			ID:      "gpt-4o",
			Billing: &copilot.ModelBilling{Multiplier: 1.0},
		},
	})

	output := model.View()

	if !strings.Contains(output, "auto") {
		t.Error("expected 'auto' in status bar for auto mode")
	}

	if !strings.Contains(output, "gpt-4o") {
		t.Error("expected resolved model 'gpt-4o' in status bar")
	}
}

// TestView_Quitting tests the goodbye view when quitting.
func TestView_Quitting(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())

	// Trigger quit by pressing Ctrl+C without permission/picker modals
	var updatedModel tea.Model = model

	updatedModel, _ = updatedModel.Update(tea.KeyMsg{Type: tea.KeyCtrlC})

	output := updatedModel.View()

	if !strings.Contains(output, "Goodbye") {
		t.Error("expected goodbye message when quitting")
	}
}

// TestModeRef_BasicOperations tests ModeRef creation and state management.
func TestModeRef_BasicOperations(t *testing.T) {
	t.Parallel()

	ref := chat.NewModeRef(false)

	if ref.IsEnabled() {
		t.Error("expected mode to be disabled initially")
	}

	ref.SetEnabled(true)

	if !ref.IsEnabled() {
		t.Error("expected mode to be enabled after SetEnabled(true)")
	}

	ref.SetEnabled(false)

	if ref.IsEnabled() {
		t.Error("expected mode to be disabled after SetEnabled(false)")
	}
}

// TestNewModel_DefaultValues tests that NewModel sets correct defaults.
func TestNewModel_DefaultValues(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())

	output := model.View()

	// Should contain the default welcome message
	if !strings.Contains(output, "Type a message") {
		t.Error("expected welcome message in initial view")
	}

	// Should contain the KSail logo
	if !strings.Contains(output, "KSail") || !strings.Contains(output, "██") {
		t.Error("expected KSail logo in initial view")
	}
}

// TestNewModel_NilThemeUsesDefaults tests that nil theme falls back to defaults.
func TestNewModel_NilThemeUsesDefaults(t *testing.T) {
	t.Parallel()

	params := newTestParams()
	params.Theme = chat.ThemeConfig{} // Zero value should trigger defaults

	model := chat.NewModel(params)

	output := model.View()

	// Should still render (default theme applied)
	if output == "" {
		t.Error("expected non-empty view with default theme")
	}
}

// TestNewModel_NilToolDisplayUsesDefaults tests that nil tool display falls back to defaults.
func TestNewModel_NilToolDisplayUsesDefaults(t *testing.T) {
	t.Parallel()

	params := newTestParams()
	params.ToolDisplay = chat.ToolDisplayConfig{} // Zero value should trigger defaults

	model := chat.NewModel(params)

	output := model.View()

	if output == "" {
		t.Error("expected non-empty view with default tool display config")
	}
}

// TestModelPickerCheckmark_CurrentModel tests that checkmark appears next to current model.
func TestModelPickerCheckmark_CurrentModel(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetSessionConfigModel(model, "gpt-4o")
	chat.ExportSetCurrentModel(model, "gpt-4o")
	chat.ExportSetAvailableModels(model, []copilot.ModelInfo{
		{ID: "gpt-4o", Policy: &copilot.ModelPolicy{State: "enabled"}},
		{ID: "claude-3", Policy: &copilot.ModelPolicy{State: "enabled"}},
	})
	chat.ExportSetShowModelPicker(model, true)

	// Increase height for better visibility
	var updatedModel tea.Model = model

	updatedModel, _ = updatedModel.Update(tea.WindowSizeMsg{Width: 100, Height: 50})

	output := updatedModel.View()

	if !strings.Contains(output, "✓") {
		t.Error("expected checkmark next to current model in picker")
	}
}

// TestModelPickerAutoCheckmark tests that auto option shows checkmark when in auto mode.
func TestModelPickerAutoCheckmark(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetSessionConfigModel(model, "")
	chat.ExportSetCurrentModel(model, "")
	chat.ExportSetAvailableModels(model, []copilot.ModelInfo{
		{ID: "gpt-4o", Policy: &copilot.ModelPolicy{State: "enabled"}},
	})
	chat.ExportSetShowModelPicker(model, true)

	output := model.View()

	// Auto option should have a checkmark since we're in auto mode
	if !strings.Contains(output, "✓") {
		t.Error("expected checkmark on auto option when in auto mode")
	}
}

// TestHelpOverlay_VisibleInView tests that the help overlay renders when opened.
func TestHelpOverlay_VisibleInView(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetShowHelpOverlay(model, true)

	// Increase height for overlay
	var updatedModel tea.Model = model

	updatedModel, _ = updatedModel.Update(tea.WindowSizeMsg{Width: 120, Height: 50})

	output := updatedModel.View()

	// Help overlay should show key bindings
	if !strings.Contains(output, "send") {
		t.Error("expected 'send' in help overlay")
	}

	if !strings.Contains(output, "quit") {
		t.Error("expected 'quit' in help overlay")
	}
}

// TestHelpOverlay_Close tests that pressing F1/escape closes the help overlay.
func TestHelpOverlay_Close(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetShowHelpOverlay(model, true)

	var updatedModel tea.Model = model

	// Press escape to close
	updatedModel, _ = updatedModel.Update(tea.KeyMsg{Type: tea.KeyEsc})

	output := updatedModel.View()

	// Help overlay should be closed - we should not see the help content
	// (but may still see short help in footer)
	if strings.Contains(output, "sessions") && strings.Contains(output, "newline") {
		t.Error("expected help overlay to be closed after pressing escape")
	}
}

// TestExitConfirmModal_VisibleInView tests that exit confirmation modal renders.
func TestExitConfirmModal_VisibleInView(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetConfirmExit(model, true)

	output := model.View()

	if !strings.Contains(output, "Exit") {
		t.Error("expected 'Exit' in exit confirmation modal")
	}
}

// TestGetEventChannel tests that the event channel is accessible.
func TestGetEventChannel(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	ch := chat.ExportGetEventChannel(model)

	if ch == nil {
		t.Error("expected non-nil event channel")
	}
}

// TestInit_ReturnsCommand tests that Init returns a command.
func TestInit_ReturnsCommand(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	cmd := model.Init()

	if cmd == nil {
		t.Error("expected non-nil command from Init()")
	}
}

// TestWindowResize_UpdatesDimensions tests that window resize events update dimensions.
func TestWindowResize_UpdatesDimensions(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())

	var updatedModel tea.Model = model

	// Send a resize message
	updatedModel, _ = updatedModel.Update(tea.WindowSizeMsg{Width: 150, Height: 60})

	output := updatedModel.View()

	// Just verify it renders without panic at the new size
	if output == "" {
		t.Error("expected non-empty view after window resize")
	}
}

// TestWindowResize_SmallTerminal tests that a very small terminal still renders.
func TestWindowResize_SmallTerminal(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())

	var updatedModel tea.Model = model

	// Send a very small resize
	updatedModel, _ = updatedModel.Update(tea.WindowSizeMsg{Width: 30, Height: 15})

	output := updatedModel.View()

	if output == "" {
		t.Error("expected non-empty view with small terminal")
	}
}

// TestUpdate_CopyFeedbackClear tests that copyFeedbackClearMsg transitions showCopyFeedback from true to false.
func TestUpdate_CopyFeedbackClear(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetShowCopyFeedback(model, true)

	if !chat.ExportGetShowCopyFeedback(model) {
		t.Fatal("expected showCopyFeedback to be true before clear message")
	}

	// Send the clear message
	clearMsg := chat.ExportNewCopyFeedbackClearMsg()

	var updatedModel tea.Model = model

	updatedModel, _ = updatedModel.Update(clearMsg)

	chatModel, ok := updatedModel.(*chat.Model)
	if !ok {
		t.Fatal("expected *chat.Model type assertion to succeed")
	}

	if chat.ExportGetShowCopyFeedback(chatModel) {
		t.Error("expected showCopyFeedback to be false after clear message")
	}
}
