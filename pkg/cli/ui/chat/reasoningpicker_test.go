package chat_test

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/devantler-tech/ksail/v7/pkg/cli/ui/chat"
	copilot "github.com/github/copilot-sdk/go"
)

// TestReasoningPickerOpen_VisibleInView tests that the reasoning picker renders when opened.
func TestReasoningPickerOpen_VisibleInView(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetShowReasoningPicker(model, true)

	output := model.View()

	if !strings.Contains(output, "Reasoning Effort") {
		t.Error("expected 'Reasoning Effort' in view when reasoning picker is open")
	}
}

// TestReasoningPickerShowsAllLevels tests that all effort levels are displayed.
func TestReasoningPickerShowsAllLevels(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetShowReasoningPicker(model, true)

	// Increase height so all 4 reasoning levels are visible in the picker
	var updatedModel tea.Model = model

	updatedModel, _ = updatedModel.Update(tea.WindowSizeMsg{Width: 100, Height: 50})

	output := updatedModel.View()

	for _, level := range []string{"off", "low", "medium", "high"} {
		if !strings.Contains(output, level) {
			t.Errorf("expected '%s' in reasoning picker view", level)
		}
	}
}

// TestReasoningPickerNavigation tests up/down navigation in the reasoning picker.
func TestReasoningPickerNavigation(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetShowReasoningPicker(model, true)
	chat.ExportSetReasoningPickerIndex(model, 0) // start at "off"

	var updatedModel tea.Model = model

	// Navigate down to "low"
	updatedModel, _ = updatedModel.Update(tea.KeyMsg{Type: tea.KeyDown})

	output := updatedModel.View()

	// "low" should be visible (and highlighted since we moved to index 1)
	if !strings.Contains(output, "low") {
		t.Error("expected 'low' to be visible in reasoning picker after navigating down")
	}
}

// TestReasoningPickerClose_Escape tests that escape closes the reasoning picker.
func TestReasoningPickerClose_Escape(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetShowReasoningPicker(model, true)

	var updatedModel tea.Model = model

	updatedModel, _ = updatedModel.Update(tea.KeyMsg{Type: tea.KeyEsc})

	output := updatedModel.View()

	if strings.Contains(output, "Reasoning Effort") {
		t.Error("expected reasoning picker to be closed after pressing escape")
	}
}

// TestReasoningPickerClose_CtrlE tests that Ctrl+E closes the reasoning picker.
func TestReasoningPickerClose_CtrlE(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetShowReasoningPicker(model, true)

	var updatedModel tea.Model = model

	updatedModel, _ = updatedModel.Update(tea.KeyMsg{Type: tea.KeyCtrlE})

	output := updatedModel.View()

	if strings.Contains(output, "Reasoning Effort") {
		t.Error("expected reasoning picker to be closed after pressing Ctrl+E")
	}
}

// TestReasoningPickerCurrentEffortCheckmark tests that the current effort level shows a checkmark.
func TestReasoningPickerCurrentEffortCheckmark(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		effort      string
		expected    string
		pickerIndex int
	}{
		{name: "off shows checkmark", effort: "", expected: "off", pickerIndex: 0},
		{name: "low shows checkmark", effort: "low", expected: "low", pickerIndex: 1},
		{name: "medium shows checkmark", effort: "medium", expected: "medium", pickerIndex: 2},
		{name: "high shows checkmark", effort: "high", expected: "high", pickerIndex: 3},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			model := chat.NewModel(newTestParams())
			chat.ExportSetSessionConfigReasoningEffort(model, testCase.effort)
			chat.ExportSetShowReasoningPicker(model, true)
			// Set picker index to match the effort level so it's visible in the scroll window
			chat.ExportSetReasoningPickerIndex(model, testCase.pickerIndex)

			// Increase height so all 4 reasoning levels are visible
			var updatedModel tea.Model = model

			updatedModel, _ = updatedModel.Update(tea.WindowSizeMsg{Width: 100, Height: 50})

			output := updatedModel.View()

			// The current effort level should be present with a checkmark
			if !strings.Contains(output, testCase.expected) {
				t.Errorf("expected '%s' effort level in reasoning picker view", testCase.expected)
			}

			if !strings.Contains(output, "✓") {
				t.Errorf(
					"expected checkmark in reasoning picker for current effort '%s'",
					testCase.expected,
				)
			}
		})
	}
}

// TestReasoningPickerSelectSameEffort tests selecting the current effort dismisses picker.
func TestReasoningPickerSelectSameEffort(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	// Current effort is "" (off), index 0 is "off"
	chat.ExportSetSessionConfigReasoningEffort(model, "")
	chat.ExportSetShowReasoningPicker(model, true)
	chat.ExportSetReasoningPickerIndex(model, 0) // "off"

	var updatedModel tea.Model = model

	updatedModel, _ = updatedModel.Update(tea.KeyMsg{Type: tea.KeyEnter})

	output := updatedModel.View()

	if strings.Contains(output, "Reasoning Effort") {
		t.Error("expected reasoning picker to close after selecting same effort level")
	}
}

// TestReasoningEffortInStatusBar tests that reasoning effort appears in the status bar.
func TestReasoningEffortInStatusBar(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		effort   string
		expected string
	}{
		{name: "no effort shows nothing", effort: "", expected: ""},
		{name: "low effort in status", effort: "low", expected: "low effort"},
		{name: "medium effort in status", effort: "medium", expected: "medium effort"},
		{name: "high effort in status", effort: "high", expected: "high effort"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			model := chat.NewModel(newTestParams())
			chat.ExportSetSessionConfigReasoningEffort(model, testCase.effort)

			output := model.View()

			if testCase.expected == "" {
				if strings.Contains(output, "effort") {
					t.Error("expected no effort indicator in status bar when effort is empty")
				}
			} else {
				if !strings.Contains(output, testCase.expected) {
					t.Errorf("expected '%s' in status bar", testCase.expected)
				}
			}
		})
	}
}

// TestReasoningPickerSupportsReasoning_Enabled tests reasoning-capable models.
func TestReasoningPickerSupportsReasoning_Enabled(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetAvailableModels(model, []copilot.ModelInfo{{
		ID: "o1-preview",
		Capabilities: copilot.ModelCapabilities{
			Supports: copilot.ModelSupports{ReasoningEffort: true},
		},
	}})
	chat.ExportSetCurrentModel(model, "o1-preview")
	chat.ExportSetSessionConfigModel(model, "o1-preview")

	if !chat.ExportCurrentModelSupportsReasoning(model) {
		t.Error("expected currentModelSupportsReasoning() = true for reasoning-capable model")
	}
}

// TestReasoningPickerSupportsReasoning_Disabled tests non-reasoning models.
func TestReasoningPickerSupportsReasoning_Disabled(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetAvailableModels(model, []copilot.ModelInfo{{
		ID: "gpt-4o",
		Capabilities: copilot.ModelCapabilities{
			Supports: copilot.ModelSupports{ReasoningEffort: false},
		},
	}})
	chat.ExportSetCurrentModel(model, "gpt-4o")
	chat.ExportSetSessionConfigModel(model, "gpt-4o")

	if chat.ExportCurrentModelSupportsReasoning(model) {
		t.Error("expected currentModelSupportsReasoning() = false for non-reasoning model")
	}
}
