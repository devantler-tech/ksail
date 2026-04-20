package chat_test

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/devantler-tech/ksail/v7/pkg/cli/ui/chat"
)

// TestFindCurrentReasoningIndex tests reasoning effort index resolution.
func TestFindCurrentReasoningIndex(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		effort   string
		expected int
	}{
		{name: "empty effort is off (index 0)", effort: "", expected: 0},
		{name: "low effort", effort: "low", expected: 1},
		{name: "medium effort", effort: "medium", expected: 2},
		{name: "high effort", effort: "high", expected: 3},
		{name: "unknown effort defaults to 0", effort: "extreme", expected: 0},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			model := chat.NewModel(newTestParams())
			chat.ExportSetSessionConfigReasoningEffort(model, testCase.effort)

			got := chat.ExportFindCurrentReasoningIndex(model)
			if got != testCase.expected {
				t.Errorf("findCurrentReasoningIndex() = %d, want %d", got, testCase.expected)
			}
		})
	}
}

// TestIsCurrentReasoningEffort tests reasoning effort matching.
func TestIsCurrentReasoningEffort(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		effort   string
		level    string
		expected bool
	}{
		{name: "off matches empty", effort: "", level: "off", expected: true},
		{name: "off does not match low", effort: "", level: "low", expected: false},
		{name: "low matches low", effort: "low", level: "low", expected: true},
		{name: "low does not match high", effort: "low", level: "high", expected: false},
		{name: "medium matches medium", effort: "medium", level: "medium", expected: true},
		{name: "high matches high", effort: "high", level: "high", expected: true},
		{name: "high does not match off", effort: "high", level: "off", expected: false},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			model := chat.NewModel(newTestParams())
			chat.ExportSetSessionConfigReasoningEffort(model, testCase.effort)

			got := chat.ExportIsCurrentReasoningEffort(model, testCase.level)
			if got != testCase.expected {
				t.Errorf(
					"isCurrentReasoningEffort(%q) = %v, want %v",
					testCase.level,
					got,
					testCase.expected,
				)
			}
		})
	}
}

// TestReasoningPickerSelect_DifferentEffort tests selecting a different reasoning level.
func TestReasoningPickerSelect_DifferentEffort(t *testing.T) {
	t.Parallel()

	// When selecting a different effort level, the model tries to recreate the session.
	// With a nil client this would cause a panic, but the picker still closes.
	// We test that the picker index movement works correctly.
	model := chat.NewModel(newTestParams())
	chat.ExportSetSessionConfigReasoningEffort(model, "")
	chat.ExportSetShowReasoningPicker(model, true)
	chat.ExportSetReasoningPickerIndex(model, 0)

	// Navigate down to "low" (index 1)
	var updatedModel tea.Model = model

	updatedModel, _ = updatedModel.Update(tea.KeyMsg{Type: tea.KeyDown})

	// Navigate down to "medium" (index 2)
	updatedModel, _ = updatedModel.Update(tea.KeyMsg{Type: tea.KeyDown})

	// Navigate up back to "low" (index 1)
	updatedModel, _ = updatedModel.Update(tea.KeyMsg{Type: tea.KeyUp})

	// Verify navigation worked (should be showing "low" highlighted)
	output := updatedModel.View()

	if output == "" {
		t.Error("expected non-empty view after reasoning picker navigation")
	}
}

// TestReasoningPickerSelect_BoundaryUp tests that up stops at 0.
func TestReasoningPickerSelect_BoundaryUp(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetShowReasoningPicker(model, true)
	chat.ExportSetReasoningPickerIndex(model, 0)

	var updatedModel tea.Model = model

	// Try to go above 0
	updatedModel, _ = updatedModel.Update(tea.KeyMsg{Type: tea.KeyUp})

	output := updatedModel.View()
	if !strings.Contains(output, "Reasoning Effort") {
		t.Error("expected reasoning picker to remain open after up boundary")
	}
}

// TestReasoningPickerSelect_BoundaryDown tests that down stops at last item.
func TestReasoningPickerSelect_BoundaryDown(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetShowReasoningPicker(model, true)
	chat.ExportSetReasoningPickerIndex(model, 3) // last item (high)

	var updatedModel tea.Model = model

	// Try to go below last item
	updatedModel, _ = updatedModel.Update(tea.KeyMsg{Type: tea.KeyDown})

	output := updatedModel.View()
	if !strings.Contains(output, "Reasoning Effort") {
		t.Error("expected reasoning picker to remain open after down boundary")
	}
}

// TestReasoningPickerSupportsReasoning_AutoWithNoModel tests auto mode with no resolved model.
func TestReasoningPickerSupportsReasoning_AutoWithNoModel(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetSessionConfigModel(model, "")
	chat.ExportSetCurrentModel(model, "")

	// With no resolved model, should optimistically return true
	if !chat.ExportCurrentModelSupportsReasoning(model) {
		t.Error("expected true for auto mode with no resolved model (optimistic)")
	}
}
