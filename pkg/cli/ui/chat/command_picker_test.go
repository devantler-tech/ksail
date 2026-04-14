package chat_test

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/devantler-tech/ksail/v6/pkg/cli/ui/chat"
	copilot "github.com/github/copilot-sdk/go"
	"github.com/stretchr/testify/assert"
)

// --- updateCommandPicker tests ---

func TestUpdateCommandPicker_ShowsOnSlash(t *testing.T) {
	t.Parallel()

	m := chat.NewModel(newCommandPickerTestParams())
	setTestCommands(m)

	chat.ExportSetTextareaValue(m, "/")
	chat.ExportUpdateCommandPicker(m)

	assert.True(t, chat.ExportShowCommandPicker(m))
	assert.Equal(t, 6, len(chat.ExportFilteredCommands(m))) // all commands match
}

func TestUpdateCommandPicker_FiltersOnPartialInput(t *testing.T) {
	t.Parallel()

	m := chat.NewModel(newCommandPickerTestParams())
	setTestCommands(m)

	chat.ExportSetTextareaValue(m, "/mo")
	chat.ExportUpdateCommandPicker(m)

	assert.True(t, chat.ExportShowCommandPicker(m))

	filtered := chat.ExportFilteredCommands(m)
	assert.Equal(t, 2, len(filtered)) // /mode, /model

	names := make([]string, len(filtered))
	for i, c := range filtered {
		names[i] = c.Name
	}

	assert.Contains(t, names, "mode")
	assert.Contains(t, names, "model")
}

func TestUpdateCommandPicker_HidesWhenNoMatch(t *testing.T) {
	t.Parallel()

	m := chat.NewModel(newCommandPickerTestParams())
	setTestCommands(m)

	chat.ExportSetTextareaValue(m, "/xyz")
	chat.ExportUpdateCommandPicker(m)

	assert.False(t, chat.ExportShowCommandPicker(m))
}

func TestUpdateCommandPicker_HidesOnSpaceAfterCommand(t *testing.T) {
	t.Parallel()

	m := chat.NewModel(newCommandPickerTestParams())
	setTestCommands(m)

	chat.ExportSetTextareaValue(m, "/mode plan")
	chat.ExportUpdateCommandPicker(m)

	assert.False(t, chat.ExportShowCommandPicker(m))
}

func TestUpdateCommandPicker_HidesOnEmptyInput(t *testing.T) {
	t.Parallel()

	m := chat.NewModel(newCommandPickerTestParams())
	setTestCommands(m)

	chat.ExportSetTextareaValue(m, "")
	chat.ExportUpdateCommandPicker(m)

	assert.False(t, chat.ExportShowCommandPicker(m))
}

func TestUpdateCommandPicker_HidesOnNonSlashInput(t *testing.T) {
	t.Parallel()

	m := chat.NewModel(newCommandPickerTestParams())
	setTestCommands(m)

	chat.ExportSetTextareaValue(m, "hello")
	chat.ExportUpdateCommandPicker(m)

	assert.False(t, chat.ExportShowCommandPicker(m))
}

func TestUpdateCommandPicker_ClampsIndex(t *testing.T) {
	t.Parallel()

	m := chat.NewModel(newCommandPickerTestParams())
	setTestCommands(m)

	// First show all commands
	chat.ExportSetTextareaValue(m, "/")
	chat.ExportUpdateCommandPicker(m)

	// Navigate to last item
	chat.ExportSetCommandPickerIndex(m, 5)

	// Now filter to fewer items
	chat.ExportSetTextareaValue(m, "/cl")
	chat.ExportUpdateCommandPicker(m)

	assert.True(t, chat.ExportShowCommandPicker(m))
	assert.Equal(t, 0, chat.ExportCommandPickerIndex(m)) // clamped to 0
}

func TestUpdateCommandPicker_CaseInsensitive(t *testing.T) {
	t.Parallel()

	m := chat.NewModel(newCommandPickerTestParams())
	setTestCommands(m)

	chat.ExportSetTextareaValue(m, "/MO")
	chat.ExportUpdateCommandPicker(m)

	assert.True(t, chat.ExportShowCommandPicker(m))
	assert.Equal(t, 2, len(chat.ExportFilteredCommands(m)))
}

// --- commandPickerExtraHeight tests ---

func TestCommandPickerExtraHeight_WhenHidden(t *testing.T) {
	t.Parallel()

	m := chat.NewModel(newCommandPickerTestParams())

	assert.Equal(t, 0, chat.ExportCommandPickerExtraHeight(m))
}

func TestCommandPickerExtraHeight_WhenShown(t *testing.T) {
	t.Parallel()

	m := chat.NewModel(newCommandPickerTestParams())
	setTestCommands(m)

	chat.ExportSetTextareaValue(m, "/")
	chat.ExportUpdateCommandPicker(m)

	// 6 commands + 2 for border
	assert.Equal(t, 8, chat.ExportCommandPickerExtraHeight(m))
}

// --- helpers ---

func newCommandPickerTestParams() chat.Params {
	return chat.Params{
		Session:       &copilot.Session{},
		SessionConfig: &copilot.SessionConfig{Model: "test-model"},
		CurrentModel:  "test-model",
		Theme:         chat.DefaultThemeConfig(),
		ToolDisplay:   chat.DefaultToolDisplayConfig(),
		EventChan:     make(chan tea.Msg, 100),
	}
}

func setTestCommands(m *chat.Model) {
	config := chat.ExportGetSessionConfig(m)
	config.Commands = []copilot.CommandDefinition{
		{Name: "mode", Description: "Switch chat mode"},
		{Name: "model", Description: "Switch LLM model"},
		{Name: "new", Description: "Start a new chat session"},
		{Name: "sessions", Description: "Open session picker"},
		{Name: "help", Description: "Show help"},
		{Name: "clear", Description: "Clear viewport"},
	}
}
