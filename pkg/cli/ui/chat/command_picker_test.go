package chat_test

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/devantler-tech/ksail/v7/pkg/cli/ui/chat"
	copilot "github.com/github/copilot-sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- updateCommandPicker tests ---

func TestUpdateCommandPicker_ShowsOnSlash(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newCommandPickerTestParams())
	setTestCommands(model)

	chat.ExportSetTextareaValue(model, "/")
	chat.ExportUpdateCommandPicker(model)

	assert.True(t, chat.ExportShowCommandPicker(model))
	assert.Len(t, chat.ExportFilteredCommands(model), 6) // all commands match
}

func TestUpdateCommandPicker_FiltersOnPartialInput(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newCommandPickerTestParams())
	setTestCommands(model)

	chat.ExportSetTextareaValue(model, "/mo")
	chat.ExportUpdateCommandPicker(model)

	assert.True(t, chat.ExportShowCommandPicker(model))

	filtered := chat.ExportFilteredCommands(model)
	assert.Len(t, filtered, 2) // /mode, /model

	names := make([]string, len(filtered))
	for i, c := range filtered {
		names[i] = c.Name
	}

	assert.Contains(t, names, "mode")
	assert.Contains(t, names, "model")
}

func TestUpdateCommandPicker_HidesWhenNoMatch(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newCommandPickerTestParams())
	setTestCommands(model)

	chat.ExportSetTextareaValue(model, "/xyz")
	chat.ExportUpdateCommandPicker(model)

	assert.False(t, chat.ExportShowCommandPicker(model))
}

func TestUpdateCommandPicker_HidesOnSpaceAfterCommand(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newCommandPickerTestParams())
	setTestCommands(model)

	// /mode has options, so command picker hides but option picker shows
	chat.ExportSetTextareaValue(model, "/mode plan")
	chat.ExportUpdateCommandPicker(model)

	assert.False(t, chat.ExportShowCommandPicker(model))
	// Option picker should be active for commands with options
	assert.True(t, chat.ExportShowOptionPicker(model))
}

func TestUpdateCommandPicker_HidesOnEmptyInput(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newCommandPickerTestParams())
	setTestCommands(model)

	chat.ExportSetTextareaValue(model, "")
	chat.ExportUpdateCommandPicker(model)

	assert.False(t, chat.ExportShowCommandPicker(model))
}

func TestUpdateCommandPicker_HidesOnNonSlashInput(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newCommandPickerTestParams())
	setTestCommands(model)

	chat.ExportSetTextareaValue(model, "hello")
	chat.ExportUpdateCommandPicker(model)

	assert.False(t, chat.ExportShowCommandPicker(model))
}

func TestUpdateCommandPicker_ClampsIndex(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newCommandPickerTestParams())
	setTestCommands(model)

	// First show all commands
	chat.ExportSetTextareaValue(model, "/")
	chat.ExportUpdateCommandPicker(model)

	// Navigate to last item
	chat.ExportSetCommandPickerIndex(model, 5)

	// Now filter to fewer items
	chat.ExportSetTextareaValue(model, "/cl")
	chat.ExportUpdateCommandPicker(model)

	assert.True(t, chat.ExportShowCommandPicker(model))
	assert.Equal(t, 0, chat.ExportCommandPickerIndex(model)) // clamped to 0
}

func TestUpdateCommandPicker_CaseInsensitive(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newCommandPickerTestParams())
	setTestCommands(model)

	chat.ExportSetTextareaValue(model, "/MO")
	chat.ExportUpdateCommandPicker(model)

	assert.True(t, chat.ExportShowCommandPicker(model))
	assert.Len(t, chat.ExportFilteredCommands(model), 2)
}

// --- commandPickerExtraHeight tests ---

func TestCommandPickerExtraHeight_WhenHidden(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newCommandPickerTestParams())

	assert.Equal(t, 0, chat.ExportCommandPickerExtraHeight(model))
}

func TestCommandPickerExtraHeight_WhenShown(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newCommandPickerTestParams())
	setTestCommands(model)

	chat.ExportSetTextareaValue(model, "/")
	chat.ExportUpdateCommandPicker(model)

	// 6 commands + 2 for border
	assert.Equal(t, 8, chat.ExportCommandPickerExtraHeight(model))
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
		{
			Name:        "mode",
			Description: "Switch chat mode",
			Handler:     func(_ copilot.CommandContext) error { return nil },
		},
		{
			Name:        "model",
			Description: "Switch LLM model",
			Handler:     func(_ copilot.CommandContext) error { return nil },
		},
		{
			Name:        "new",
			Description: "Start a new chat session",
			Handler:     func(_ copilot.CommandContext) error { return nil },
		},
		{
			Name:        "sessions",
			Description: "Open session picker",
			Handler:     func(_ copilot.CommandContext) error { return nil },
		},
		{
			Name:        "help",
			Description: "Show help",
			Handler:     func(_ copilot.CommandContext) error { return nil },
		},
		{
			Name:        "clear",
			Description: "Clear viewport",
			Handler:     func(_ copilot.CommandContext) error { return nil },
		},
	}
}

// --- tryDispatchSlashCommand tests ---

func TestTryDispatchSlashCommand_ValidCommand(t *testing.T) {
	t.Parallel()

	var calledArgs string

	model := chat.NewModel(newCommandPickerTestParams())
	config := chat.ExportGetSessionConfig(model)
	config.Commands = []copilot.CommandDefinition{
		{Name: "mode", Handler: func(ctx copilot.CommandContext) error {
			calledArgs = ctx.Args

			return nil
		}},
	}

	handled, _, _ := chat.ExportTryDispatchSlashCommand(model, "/mode plan")

	assert.True(t, handled)
	assert.Equal(t, "plan", calledArgs)
	assert.NoError(t, chat.ExportGetErr(model))
}

func TestTryDispatchSlashCommand_CommandWithoutArgs(t *testing.T) {
	t.Parallel()

	called := false

	model := chat.NewModel(newCommandPickerTestParams())
	config := chat.ExportGetSessionConfig(model)
	config.Commands = []copilot.CommandDefinition{
		{Name: "clear", Handler: func(_ copilot.CommandContext) error {
			called = true

			return nil
		}},
	}

	handled, _, _ := chat.ExportTryDispatchSlashCommand(model, "/clear")

	assert.True(t, handled)
	assert.True(t, called)
}

func TestTryDispatchSlashCommand_UnknownCommand(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newCommandPickerTestParams())
	setTestCommands(model)

	handled, _, _ := chat.ExportTryDispatchSlashCommand(model, "/unknown")

	assert.True(t, handled)
	require.Error(t, chat.ExportGetErr(model))
	assert.Contains(t, chat.ExportGetErr(model).Error(), "unknown command: /unknown")
}

func TestTryDispatchSlashCommand_NotSlashCommand(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newCommandPickerTestParams())
	setTestCommands(model)

	handled, _, _ := chat.ExportTryDispatchSlashCommand(model, "hello world")

	assert.False(t, handled)
}

func TestTryDispatchSlashCommand_CaseInsensitive(t *testing.T) {
	t.Parallel()

	called := false

	model := chat.NewModel(newCommandPickerTestParams())
	config := chat.ExportGetSessionConfig(model)
	config.Commands = []copilot.CommandDefinition{
		{Name: "mode", Handler: func(_ copilot.CommandContext) error {
			called = true

			return nil
		}},
	}

	handled, _, _ := chat.ExportTryDispatchSlashCommand(model, "/MODE plan")

	assert.True(t, handled)
	assert.True(t, called)
}

// --- Option picker tests ---

func TestOptionPicker_ShowsOnCommandWithSpace(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newCommandPickerTestParams())
	setTestCommands(model)

	chat.ExportSetTextareaValue(model, "/mode ")
	chat.ExportUpdateCommandPicker(model)

	assert.False(t, chat.ExportShowCommandPicker(model))
	assert.True(t, chat.ExportShowOptionPicker(model))
	assert.Equal(t, "mode", chat.ExportActiveCommandName(model))
	assert.Len(t, chat.ExportFilteredOptions(model), 3) // interactive, plan, autopilot
}

func TestOptionPicker_FiltersOnPartialArg(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newCommandPickerTestParams())
	setTestCommands(model)

	chat.ExportSetTextareaValue(model, "/mode pl")
	chat.ExportUpdateCommandPicker(model)

	assert.True(t, chat.ExportShowOptionPicker(model))

	filtered := chat.ExportFilteredOptions(model)
	assert.Len(t, filtered, 1)
	assert.Equal(t, "plan", filtered[0].Name)
}

func TestOptionPicker_HidesForCommandWithoutOptions(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newCommandPickerTestParams())
	setTestCommands(model)

	chat.ExportSetTextareaValue(model, "/clear ")
	chat.ExportUpdateCommandPicker(model)

	assert.False(t, chat.ExportShowCommandPicker(model))
	assert.False(t, chat.ExportShowOptionPicker(model))
}

func TestOptionPicker_ClampsIndex(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newCommandPickerTestParams())
	setTestCommands(model)

	// Show all options
	chat.ExportSetTextareaValue(model, "/mode ")
	chat.ExportUpdateCommandPicker(model)

	// Set index beyond what filter will produce
	chat.ExportSetOptionPickerIndex(model, 2)

	// Now filter to single option
	chat.ExportSetTextareaValue(model, "/mode pl")
	chat.ExportUpdateCommandPicker(model)

	assert.True(t, chat.ExportShowOptionPicker(model))
	assert.Equal(t, 0, chat.ExportOptionPickerIndex(model))
}

func TestOptionPicker_CaseInsensitive(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newCommandPickerTestParams())
	setTestCommands(model)

	chat.ExportSetTextareaValue(model, "/mode PL")
	chat.ExportUpdateCommandPicker(model)

	assert.True(t, chat.ExportShowOptionPicker(model))
	assert.Len(t, chat.ExportFilteredOptions(model), 1)
}

// --- pickerExtraHeight tests ---

func TestPickerExtraHeight_OptionPicker(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newCommandPickerTestParams())
	setTestCommands(model)

	chat.ExportSetTextareaValue(model, "/mode ")
	chat.ExportUpdateCommandPicker(model)

	// 3 options + 2 for border
	assert.Equal(t, 5, chat.ExportPickerExtraHeight(model))
}
