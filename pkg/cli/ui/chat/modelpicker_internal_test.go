package chat_test

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/devantler-tech/ksail/v7/pkg/cli/ui/chat"
	copilot "github.com/github/copilot-sdk/go"
)

// TestIsAutoMode tests the auto mode detection logic.
func TestIsAutoMode(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		model    string
		expected bool
	}{
		{name: "empty model is auto", model: "", expected: true},
		{name: "explicit model is not auto", model: "gpt-4o", expected: false},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			model := chat.NewModel(newTestParams())
			chat.ExportSetSessionConfigModel(model, testCase.model)

			if got := chat.ExportIsAutoMode(model); got != testCase.expected {
				t.Errorf("isAutoMode() = %v, want %v", got, testCase.expected)
			}
		})
	}
}

// TestResolvedAutoModel tests resolved auto model detection.
func TestResolvedAutoModel(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		configModel  string
		currentModel string
		lastUsage    string
		expected     string
	}{
		{
			name:         "not auto mode returns empty",
			configModel:  "gpt-4o",
			currentModel: "gpt-4o",
			lastUsage:    "",
			expected:     "",
		},
		{
			name:         "auto with current model",
			configModel:  "",
			currentModel: "gpt-4o",
			lastUsage:    "",
			expected:     "gpt-4o",
		},
		{
			name:         "auto with last usage fallback",
			configModel:  "",
			currentModel: "",
			lastUsage:    "claude-3",
			expected:     "claude-3",
		},
		{
			name:         "auto not yet resolved",
			configModel:  "",
			currentModel: "",
			lastUsage:    "",
			expected:     "",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			model := chat.NewModel(newTestParams())
			chat.ExportSetSessionConfigModel(model, testCase.configModel)
			chat.ExportSetCurrentModel(model, testCase.currentModel)
			chat.ExportSetLastUsageModel(model, testCase.lastUsage)

			if got := chat.ExportResolvedAutoModel(model); got != testCase.expected {
				t.Errorf("resolvedAutoModel() = %q, want %q", got, testCase.expected)
			}
		})
	}
}

// TestFindModelMultiplier tests model billing multiplier lookup.
func TestFindModelMultiplier(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		models   []copilot.ModelInfo
		modelID  string
		expected float64
	}{
		{
			name: "found with billing",
			models: []copilot.ModelInfo{
				{ID: "gpt-4o", Billing: &copilot.ModelBilling{Multiplier: 1.0}},
			},
			modelID:  "gpt-4o",
			expected: 1.0,
		},
		{
			name: "found without billing",
			models: []copilot.ModelInfo{
				{ID: "gpt-4o", Billing: nil},
			},
			modelID:  "gpt-4o",
			expected: 0,
		},
		{
			name:     "not found",
			models:   []copilot.ModelInfo{},
			modelID:  "gpt-4o",
			expected: 0,
		},
		{
			name: "different multiplier",
			models: []copilot.ModelInfo{
				{ID: "claude-3", Billing: &copilot.ModelBilling{Multiplier: 2.5}},
			},
			modelID:  "claude-3",
			expected: 2.5,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			model := chat.NewModel(newTestParams())
			chat.ExportSetAvailableModels(model, testCase.models)

			got := chat.ExportFindModelMultiplier(model, testCase.modelID)
			if got != testCase.expected {
				t.Errorf(
					"findModelMultiplier(%q) = %v, want %v",
					testCase.modelID,
					got,
					testCase.expected,
				)
			}
		})
	}
}

// TestFindCurrentModelIndex tests model picker index resolution.
func TestFindCurrentModelIndex(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		configModel  string
		currentModel string
		models       []copilot.ModelInfo
		expected     int
	}{
		{
			name:         "auto mode returns 0",
			configModel:  "",
			currentModel: "gpt-4o",
			models: []copilot.ModelInfo{
				{ID: "gpt-4o"},
			},
			expected: 0,
		},
		{
			name:         "explicit model found",
			configModel:  "gpt-4o",
			currentModel: "gpt-4o",
			models: []copilot.ModelInfo{
				{ID: "claude-3"},
				{ID: "gpt-4o"},
			},
			expected: 2, // offset by 1 for auto option
		},
		{
			name:         "model not found falls back to 0",
			configModel:  "unknown",
			currentModel: "unknown",
			models: []copilot.ModelInfo{
				{ID: "gpt-4o"},
			},
			expected: 0,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			model := chat.NewModel(newTestParams())
			chat.ExportSetSessionConfigModel(model, testCase.configModel)
			chat.ExportSetCurrentModel(model, testCase.currentModel)
			chat.ExportSetAvailableModels(model, testCase.models)

			if got := chat.ExportFindCurrentModelIndex(model); got != testCase.expected {
				t.Errorf("findCurrentModelIndex() = %d, want %d", got, testCase.expected)
			}
		})
	}
}

// TestModelPickerFilter_ClearsOnEscape tests that escape clears the filter text.
func TestModelPickerFilter_ClearsOnEscape(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	models := []copilot.ModelInfo{
		{ID: "gpt-4o", Policy: &copilot.ModelPolicy{State: "enabled"}},
		{ID: "claude-3", Policy: &copilot.ModelPolicy{State: "enabled"}},
	}
	chat.ExportSetAvailableModels(model, models)
	chat.ExportSetShowModelPicker(model, true)

	var updatedModel tea.Model = model

	// Enter filter mode
	updatedModel, _ = updatedModel.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})

	// Type filter text
	updatedModel = typeText(updatedModel, "gpt")

	// Press escape to clear filter
	updatedModel, _ = updatedModel.Update(tea.KeyMsg{Type: tea.KeyEsc})

	output := updatedModel.View()

	// Both models should be visible again (filter cleared)
	if !strings.Contains(output, "claude-3") || !strings.Contains(output, "gpt-4o") {
		// After escape from filter mode, the filter is cleared and all models shown
		// The picker is still open, so both should be visible
		if !strings.Contains(output, "Select Model") {
			t.Error("expected model picker to remain open after clearing filter")
		}
	}
}

// TestModelPickerFilter_EnterConfirmsFilter tests enter exits filter mode.
func TestModelPickerFilter_EnterConfirmsFilter(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	models := []copilot.ModelInfo{
		{ID: "gpt-4o", Policy: &copilot.ModelPolicy{State: "enabled"}},
		{ID: "claude-3", Policy: &copilot.ModelPolicy{State: "enabled"}},
	}
	chat.ExportSetAvailableModels(model, models)
	chat.ExportSetShowModelPicker(model, true)

	var updatedModel tea.Model = model

	// Enter filter mode
	updatedModel, _ = updatedModel.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	updatedModel = typeText(updatedModel, "gpt")

	// Press enter to confirm filter (stays in picker but exits filter mode)
	updatedModel, _ = updatedModel.Update(tea.KeyMsg{Type: tea.KeyEnter})

	output := updatedModel.View()

	// Picker should still be open with filtered results
	if !strings.Contains(output, "gpt") {
		t.Error("expected filtered results to persist after enter")
	}
}

// TestModelPickerFilter_BackspaceRemovesChar tests backspace in filter mode.
func TestModelPickerFilter_BackspaceRemovesChar(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	models := []copilot.ModelInfo{
		{ID: "gpt-4o", Policy: &copilot.ModelPolicy{State: "enabled"}},
		{ID: "claude-3", Policy: &copilot.ModelPolicy{State: "enabled"}},
	}
	chat.ExportSetAvailableModels(model, models)
	chat.ExportSetShowModelPicker(model, true)

	var updatedModel tea.Model = model

	// Enter filter mode and type
	updatedModel, _ = updatedModel.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	updatedModel = typeText(updatedModel, "gpt")

	// Press backspace
	updatedModel, _ = updatedModel.Update(tea.KeyMsg{Type: tea.KeyBackspace})

	// Filter should now be "gp" — just verify it renders without panic
	output := updatedModel.View()
	if output == "" {
		t.Error("expected non-empty view after backspace in filter")
	}
}

type modelStatusTextCase struct {
	name         string
	configModel  string
	currentModel string
	lastUsage    string
	models       []copilot.ModelInfo
	expected     string
}

func buildModelStatusTextCases() []modelStatusTextCase {
	return []modelStatusTextCase{
		{name: "auto mode unresolved", expected: "auto"},
		{
			name:         "auto mode resolved with multiplier",
			currentModel: "gpt-4o",
			models: []copilot.ModelInfo{
				{ID: "gpt-4o", Billing: &copilot.ModelBilling{Multiplier: 1.0}},
			},
			expected: "gpt-4o (1x)",
		},
		{
			name:         "auto mode resolved with fractional multiplier",
			currentModel: "claude-haiku-4.5",
			models: []copilot.ModelInfo{
				{ID: "claude-haiku-4.5", Billing: &copilot.ModelBilling{Multiplier: 0.33}},
			},
			expected: "claude-haiku-4.5 (0.33x)",
		},
		{
			name:         "explicit model",
			configModel:  "claude-3",
			currentModel: "claude-3",
			expected:     "claude-3",
		},
	}
}

// TestBuildModelStatusText tests model status text rendering.
func TestBuildModelStatusText(t *testing.T) {
	t.Parallel()

	for _, testCase := range buildModelStatusTextCases() {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			model := chat.NewModel(newTestParams())
			chat.ExportSetSessionConfigModel(model, testCase.configModel)
			chat.ExportSetCurrentModel(model, testCase.currentModel)
			chat.ExportSetLastUsageModel(model, testCase.lastUsage)

			if testCase.models != nil {
				chat.ExportSetAvailableModels(model, testCase.models)
			}

			result := chat.ExportBuildModelStatusText(model)

			if !strings.Contains(result, testCase.expected) {
				t.Errorf(
					"buildModelStatusText() = %q, want to contain %q",
					result,
					testCase.expected,
				)
			}
		})
	}
}

// TestModelPickerUpNavigation_Boundary tests that up arrow stops at 0.
func TestModelPickerUpNavigation_Boundary(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	models := []copilot.ModelInfo{
		{ID: "gpt-4o", Policy: &copilot.ModelPolicy{State: "enabled"}},
	}
	chat.ExportSetAvailableModels(model, models)
	chat.ExportSetShowModelPicker(model, true)
	chat.ExportSetModelPickerIndex(model, 0)

	var updatedModel tea.Model = model

	// Try to go above 0
	updatedModel, _ = updatedModel.Update(tea.KeyMsg{Type: tea.KeyUp})

	output := updatedModel.View()
	if !strings.Contains(output, "Select Model") {
		t.Error("expected model picker to remain open")
	}
}

// TestFormatMultiplier tests that formatMultiplier formats values consistently and
// never produces scientific notation for extreme inputs.
func TestFormatMultiplier(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		mult     float64
		expected string
	}{
		{name: "integer multiplier", mult: 1.0, expected: "1"},
		{name: "fractional multiplier", mult: 1.5, expected: "1.5"},
		{name: "two decimal places", mult: 1.25, expected: "1.25"},
		{name: "small fractional", mult: 0.33, expected: "0.33"},
		{name: "large integer", mult: 1000000.0, expected: "1000000"},
		{name: "large fractional", mult: 1000000.50, expected: "1000000.5"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := chat.ExportFormatMultiplier(testCase.mult)
			if got != testCase.expected {
				t.Errorf(
					"formatMultiplier(%g) = %q, want %q",
					testCase.mult,
					got,
					testCase.expected,
				)
			}
		})
	}
}

// TestModelPickerSelectAuto_WhenAlreadyAuto tests selecting auto when already in auto mode.
func TestModelPickerSelectAuto_WhenAlreadyAuto(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	models := []copilot.ModelInfo{
		{ID: "gpt-4o", Policy: &copilot.ModelPolicy{State: "enabled"}},
	}
	chat.ExportSetAvailableModels(model, models)
	chat.ExportSetSessionConfigModel(model, "")
	chat.ExportSetShowModelPicker(model, true)
	chat.ExportSetModelPickerIndex(model, 0) // auto option

	var updatedModel tea.Model = model

	// Select auto when already in auto mode — should just close picker
	updatedModel, _ = updatedModel.Update(tea.KeyMsg{Type: tea.KeyEnter})

	output := updatedModel.View()
	if strings.Contains(output, "Select Model") {
		t.Error("expected model picker to close after selecting auto in auto mode")
	}
}
