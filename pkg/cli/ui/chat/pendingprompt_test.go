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

// typeText sends each rune in text as a KeyRunes message.
func typeText(m tea.Model, text string) tea.Model {
	for _, char := range text {
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{char}})
	}

	return m
}

// ctrlQKey returns a KeyMsg for Ctrl+Q (queue keybind).
func ctrlQKey() tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyCtrlQ}
}

func TestQueuePrompt_VisibleInView(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	model.SetStreaming(true)

	updatedModel := typeText(model, "hello world")

	// Press Ctrl+Q to queue (only works during streaming)
	updatedModel, _ = updatedModel.Update(ctrlQKey())

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
	model.SetStreaming(true)

	updatedModel := typeText(model, "steer this")

	// Press Enter to steer (Enter steers during streaming)
	updatedModel, _ = updatedModel.Update(tea.KeyMsg{Type: tea.KeyEnter})

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
	model.SetStreaming(true)

	updatedModel := typeText(model, "test prompt")

	// Queue a prompt (requires streaming)
	updatedModel, _ = updatedModel.Update(ctrlQKey())

	// Delete it
	updatedModel, _ = updatedModel.Update(tea.KeyMsg{Type: tea.KeyCtrlD})

	output := updatedModel.View()

	if strings.Contains(output, "Pending Prompts") {
		t.Error("expected no 'Pending Prompts' section after deleting all pending prompts")
	}
}

func TestQueuePrompt_IgnoredWhenNotStreaming(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())

	updatedModel := typeText(model, "hello")

	// Press Ctrl+Q when NOT streaming — should be ignored
	updatedModel, _ = updatedModel.Update(ctrlQKey())

	output := updatedModel.View()

	if strings.Contains(output, "Pending Prompts") {
		t.Error("queueing should be ignored when not streaming")
	}
}

func TestEmptyInput_IgnoredByQueue(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	model.SetStreaming(true)

	var updatedModel tea.Model = model

	// Press Ctrl+Q with empty textarea
	updatedModel, _ = updatedModel.Update(ctrlQKey())

	output := updatedModel.View()

	if strings.Contains(output, "Pending Prompts") {
		t.Error("empty input should not create a pending prompt")
	}
}

func TestEmptyInput_IgnoredBySteer(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	model.SetStreaming(true)

	var updatedModel tea.Model = model

	// Press Enter with empty textarea during streaming
	updatedModel, _ = updatedModel.Update(tea.KeyMsg{Type: tea.KeyEnter})

	output := updatedModel.View()

	if strings.Contains(output, "Pending Prompts") {
		t.Error("empty input should not create a steering prompt")
	}
}

func TestMultipleQueuedPrompts_ShowNumbered(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	model.SetStreaming(true)

	updatedModel := typeText(model, "first task")

	updatedModel, _ = updatedModel.Update(ctrlQKey())

	updatedModel = typeText(updatedModel, "second task")

	updatedModel, _ = updatedModel.Update(ctrlQKey())

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
	model.SetStreaming(true)

	updatedModel := typeText(model, "test")

	updatedModel, _ = updatedModel.Update(ctrlQKey())

	output := updatedModel.View()

	if !strings.Contains(output, "1 pending") {
		t.Error("expected '1 pending' in footer after queueing a prompt")
	}
}
