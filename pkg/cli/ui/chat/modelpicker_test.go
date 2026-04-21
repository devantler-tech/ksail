package chat_test

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/devantler-tech/ksail/v7/pkg/cli/ui/chat"
	copilot "github.com/github/copilot-sdk/go"
)

// TestFilterEnabledModels tests filtering models by policy state.
func TestFilterEnabledModels(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		models   []copilot.ModelInfo
		expected int
	}{
		{
			name:     "empty list",
			models:   nil,
			expected: 0,
		},
		{
			name: "all enabled",
			models: []copilot.ModelInfo{
				{ID: "gpt-4o", Policy: &copilot.ModelPolicy{State: "enabled"}},
				{ID: "claude-3", Policy: &copilot.ModelPolicy{State: "enabled"}},
			},
			expected: 2,
		},
		{
			name: "some disabled",
			models: []copilot.ModelInfo{
				{ID: "gpt-4o", Policy: &copilot.ModelPolicy{State: "enabled"}},
				{ID: "gpt-3.5", Policy: &copilot.ModelPolicy{State: "disabled"}},
				{ID: "claude-3", Policy: &copilot.ModelPolicy{State: "enabled"}},
			},
			expected: 2,
		},
		{
			name: "nil policy excluded",
			models: []copilot.ModelInfo{
				{ID: "gpt-4o", Policy: nil},
				{ID: "claude-3", Policy: &copilot.ModelPolicy{State: "enabled"}},
			},
			expected: 1,
		},
		{
			name: "none enabled",
			models: []copilot.ModelInfo{
				{ID: "gpt-4o", Policy: &copilot.ModelPolicy{State: "disabled"}},
			},
			expected: 0,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := chat.FilterEnabledModels(testCase.models)
			if len(result) != testCase.expected {
				t.Errorf(
					"FilterEnabledModels() returned %d models, want %d",
					len(result),
					testCase.expected,
				)
			}
		})
	}
}

// TestModelPickerOpen_VisibleInView tests that the model picker renders when opened.
func TestModelPickerOpen_VisibleInView(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetAvailableModels(model, []copilot.ModelInfo{
		{ID: "gpt-4o", Policy: &copilot.ModelPolicy{State: "enabled"}},
		{ID: "claude-3", Policy: &copilot.ModelPolicy{State: "enabled"}},
	})
	chat.ExportSetShowModelPicker(model, true)

	output := model.View()

	if !strings.Contains(output, "Select Model") {
		t.Error("expected 'Select Model' in view when model picker is open")
	}

	if !strings.Contains(output, "auto") {
		t.Error("expected 'auto' option in model picker")
	}
}

// TestModelPickerNavigation tests up/down navigation in the model picker.
func TestModelPickerNavigation(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	models := []copilot.ModelInfo{
		{ID: "gpt-4o", Policy: &copilot.ModelPolicy{State: "enabled"}},
		{ID: "claude-3", Policy: &copilot.ModelPolicy{State: "enabled"}},
	}
	chat.ExportSetAvailableModels(model, models)
	chat.ExportSetShowModelPicker(model, true)
	chat.ExportSetModelPickerIndex(model, 0)

	// Navigate down
	var updatedModel tea.Model = model

	updatedModel, _ = updatedModel.Update(tea.KeyMsg{Type: tea.KeyDown})

	output := updatedModel.View()

	// The picker should highlight the second item (gpt-4o at index 1)
	if !strings.Contains(output, "gpt-4o") {
		t.Error("expected 'gpt-4o' to be visible in picker")
	}
}

// TestModelPickerClose_EscapeKey tests that escape closes the model picker.
func TestModelPickerClose_EscapeKey(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetShowModelPicker(model, true)

	var updatedModel tea.Model = model

	updatedModel, _ = updatedModel.Update(tea.KeyMsg{Type: tea.KeyEsc})

	output := updatedModel.View()

	if strings.Contains(output, "Select Model") {
		t.Error("expected model picker to be closed after pressing escape")
	}
}

// TestModelPickerClose_CtrlO tests that Ctrl+O toggles the model picker closed.
func TestModelPickerClose_CtrlO(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetShowModelPicker(model, true)

	var updatedModel tea.Model = model

	updatedModel, _ = updatedModel.Update(tea.KeyMsg{Type: tea.KeyCtrlO})

	output := updatedModel.View()

	if strings.Contains(output, "Select Model") {
		t.Error("expected model picker to be closed after pressing Ctrl+O")
	}
}

// TestModelPickerAutoResolvedDisplay tests that the auto option shows resolved model.
func TestModelPickerAutoResolvedDisplay(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	// Set auto mode (empty session config model means auto)
	chat.ExportSetSessionConfigModel(model, "")
	chat.ExportSetLastUsageModel(model, "gpt-4o")
	chat.ExportSetAvailableModels(model, []copilot.ModelInfo{
		{
			ID:      "gpt-4o",
			Policy:  &copilot.ModelPolicy{State: "enabled"},
			Billing: &copilot.ModelBilling{Multiplier: 1.0},
		},
	})
	chat.ExportSetShowModelPicker(model, true)

	output := model.View()

	// Should show "auto (gpt-4o · 1x)" with resolved model and multiplier
	if !strings.Contains(output, "auto") {
		t.Error("expected 'auto' in view when showing resolved model")
	}

	if !strings.Contains(output, "gpt-4o") {
		t.Error("expected resolved model 'gpt-4o' in auto option display")
	}
}

// TestModelPickerFilter tests model filtering with text input.
func TestModelPickerFilter(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	models := []copilot.ModelInfo{
		{ID: "gpt-4o", Policy: &copilot.ModelPolicy{State: "enabled"}},
		{ID: "claude-3", Policy: &copilot.ModelPolicy{State: "enabled"}},
		{ID: "gpt-3.5", Policy: &copilot.ModelPolicy{State: "enabled"}},
	}
	chat.ExportSetAvailableModels(model, models)
	chat.ExportSetShowModelPicker(model, true)

	var updatedModel tea.Model = model

	// Press "/" to activate filter mode
	updatedModel, _ = updatedModel.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})

	// Type filter text
	updatedModel = typeText(updatedModel, "gpt")

	output := updatedModel.View()

	// Should show GPT models
	if !strings.Contains(output, "gpt") {
		t.Error("expected 'gpt' models to be visible after filtering")
	}
}

// TestModelPickerSelectCurrentModel tests selecting the already-current model.
func TestModelPickerSelectCurrentModel(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	models := []copilot.ModelInfo{
		{ID: "gpt-4o", Policy: &copilot.ModelPolicy{State: "enabled"}},
	}
	chat.ExportSetAvailableModels(model, models)
	chat.ExportSetCurrentModel(model, "gpt-4o")
	chat.ExportSetSessionConfigModel(model, "gpt-4o")
	chat.ExportSetShowModelPicker(model, true)
	chat.ExportSetModelPickerIndex(model, 1) // gpt-4o is at index 1 (after auto)

	// Press Enter to select (same model = just close picker)
	var updatedModel tea.Model = model

	updatedModel, _ = updatedModel.Update(tea.KeyMsg{Type: tea.KeyEnter})

	output := updatedModel.View()

	if strings.Contains(output, "Select Model") {
		t.Error("expected model picker to close after selecting same model")
	}
}

// TestModelPickerItemMultiplierDisplay tests that non-auto model list items render
// their billing multiplier, including fractional values like 0.33x.
func TestModelPickerItemMultiplierDisplay(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		modelID    string
		multiplier float64
		wantSuffix string
	}{
		{
			name:       "integer multiplier",
			modelID:    "gpt-4o",
			multiplier: 1.0,
			wantSuffix: "(1x)",
		},
		{
			name:       "fractional multiplier",
			modelID:    "claude-haiku-4.5",
			multiplier: 0.33,
			wantSuffix: "(0.33x)",
		},
		{
			name:       "non-integer multiplier",
			modelID:    "claude-3-5-sonnet",
			multiplier: 2.5,
			wantSuffix: "(2.5x)",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			model := chat.NewModel(newTestParams())
			chat.ExportSetAvailableModels(model, []copilot.ModelInfo{
				{
					ID:      testCase.modelID,
					Policy:  &copilot.ModelPolicy{State: "enabled"},
					Billing: &copilot.ModelBilling{Multiplier: testCase.multiplier},
				},
			})
			chat.ExportSetShowModelPicker(model, true)

			output := model.View()

			if !strings.Contains(output, testCase.wantSuffix) {
				t.Errorf(
					"model picker item for %q with multiplier %g: expected %q in view, got:\n%s",
					testCase.modelID,
					testCase.multiplier,
					testCase.wantSuffix,
					output,
				)
			}
		})
	}
}

// TestModelPickerItemNoMultiplier tests that model list items without billing show no multiplier suffix.
func TestModelPickerItemNoMultiplier(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetAvailableModels(model, []copilot.ModelInfo{
		{
			ID:      "gpt-4o",
			Policy:  &copilot.ModelPolicy{State: "enabled"},
			Billing: nil,
		},
	})
	chat.ExportSetShowModelPicker(model, true)

	output := model.View()

	if strings.Contains(output, "gpt-4o (") {
		t.Errorf("expected no multiplier suffix for model without billing, got:\n%s", output)
	}
}
