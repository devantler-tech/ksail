package chat_test

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/devantler-tech/ksail/v5/pkg/cli/ui/chat"
	copilot "github.com/github/copilot-sdk/go"
)

// newTestParams returns minimal Params for testing queue behavior.
// Uses a mock session via copilot.Session{} zero value.
func newTestParams() chat.Params {
	return chat.Params{
		Session:       &copilot.Session{},
		SessionConfig: &copilot.SessionConfig{Model: "test-model"},
		CurrentModel:  "test-model",
		Theme:         chat.DefaultThemeConfig(),
		ToolDisplay:   chat.DefaultToolDisplayConfig(),
		EventChan:     make(chan tea.Msg, 100),
	}
}

func TestQueuePrompt_VisibleInView(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())

	// Type a prompt
	var updatedModel tea.Model = model

	for _, char := range "hello world" {
		updatedModel, _ = updatedModel.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{char}})
	}

	// Press Ctrl+Q to queue
	updatedModel, _ = updatedModel.Update(tea.KeyMsg{Type: tea.KeyCtrlQ})

	// View should show pending prompts section
	output := updatedModel.View()

	if !strings.Contains(output, "Pending Prompts") {
		t.Error("expected 'Pending Prompts' section in view after queueing")
	}

	if !strings.Contains(output, "QUEUED") {
		t.Error("expected 'QUEUED' label in view after queueing")
	}
}

func TestSteerPrompt_VisibleInView(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())

	var updatedModel tea.Model = model

	for _, char := range "steer this" {
		updatedModel, _ = updatedModel.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{char}})
	}

	// Press Ctrl+S to steer
	updatedModel, _ = updatedModel.Update(tea.KeyMsg{Type: tea.KeyCtrlS})

	output := updatedModel.View()

	if !strings.Contains(output, "Pending Prompts") {
		t.Error("expected 'Pending Prompts' section in view after steering")
	}

	if !strings.Contains(output, "STEERING") {
		t.Error("expected 'STEERING' label in view after steering")
	}
}

func TestDeletePendingPrompt_RemovesFromView(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())

	var updatedModel tea.Model = model

	// Queue a prompt
	for _, char := range "test prompt" {
		updatedModel, _ = updatedModel.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{char}})
	}

	updatedModel, _ = updatedModel.Update(tea.KeyMsg{Type: tea.KeyCtrlQ})

	// Delete it
	updatedModel, _ = updatedModel.Update(tea.KeyMsg{Type: tea.KeyCtrlD})

	// Should no longer show pending prompts section
	output := updatedModel.View()

	if strings.Contains(output, "Pending Prompts") {
		t.Error("expected no 'Pending Prompts' section after deleting all pending prompts")
	}
}

func TestEmptyInput_IgnoredByQueue(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())

	var updatedModel tea.Model = model

	// Press Ctrl+Q with empty textarea
	updatedModel, _ = updatedModel.Update(tea.KeyMsg{Type: tea.KeyCtrlQ})

	output := updatedModel.View()

	if strings.Contains(output, "Pending Prompts") {
		t.Error("empty input should not create a pending prompt")
	}
}

func TestEmptyInput_IgnoredBySteer(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())

	var updatedModel tea.Model = model

	// Press Ctrl+S with empty textarea
	updatedModel, _ = updatedModel.Update(tea.KeyMsg{Type: tea.KeyCtrlS})

	output := updatedModel.View()

	if strings.Contains(output, "Pending Prompts") {
		t.Error("empty input should not create a steering prompt")
	}
}

func TestMultipleQueuedPrompts_ShowNumbered(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())

	var updatedModel tea.Model = model

	// Queue first prompt
	for _, char := range "first task" {
		updatedModel, _ = updatedModel.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{char}})
	}

	updatedModel, _ = updatedModel.Update(tea.KeyMsg{Type: tea.KeyCtrlQ})

	// Queue second prompt
	for _, char := range "second task" {
		updatedModel, _ = updatedModel.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{char}})
	}

	updatedModel, _ = updatedModel.Update(tea.KeyMsg{Type: tea.KeyCtrlQ})

	output := updatedModel.View()

	if !strings.Contains(output, "QUEUED #1") {
		t.Error("expected 'QUEUED #1' in view")
	}

	if !strings.Contains(output, "QUEUED #2") {
		t.Error("expected 'QUEUED #2' in view")
	}
}

func TestFooterShowsPendingCount(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())

	var updatedModel tea.Model = model

	for _, char := range "test" {
		updatedModel, _ = updatedModel.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{char}})
	}

	updatedModel, _ = updatedModel.Update(tea.KeyMsg{Type: tea.KeyCtrlQ})

	output := updatedModel.View()

	if !strings.Contains(output, "1 pending") {
		t.Error("expected '1 pending' in footer after queueing a prompt")
	}
}
