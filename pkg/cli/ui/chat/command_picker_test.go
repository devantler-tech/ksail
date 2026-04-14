package chat_test

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/devantler-tech/ksail/v6/pkg/cli/ui/chat"
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
	assert.Equal(t, 6, len(chat.ExportFilteredCommands(model))) // all commands match
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
		{Name: "mode", Description: "Switch chat mode", Handler: func(_ copilot.CommandContext) error { return nil }},
		{Name: "model", Description: "Switch LLM model", Handler: func(_ copilot.CommandContext) error { return nil }},
		{Name: "new", Description: "Start a new chat session", Handler: func(_ copilot.CommandContext) error { return nil }},
		{Name: "sessions", Description: "Open session picker", Handler: func(_ copilot.CommandContext) error { return nil }},
		{Name: "help", Description: "Show help", Handler: func(_ copilot.CommandContext) error { return nil }},
		{Name: "clear", Description: "Clear viewport", Handler: func(_ copilot.CommandContext) error { return nil }},
	}
}

// --- tryDispatchSlashCommand tests ---

func TestTryDispatchSlashCommand_ValidCommand(t *testing.T) {
	t.Parallel()

	var calledArgs string

	m := chat.NewModel(newCommandPickerTestParams())
	config := chat.ExportGetSessionConfig(m)
	config.Commands = []copilot.CommandDefinition{
		{Name: "mode", Handler: func(ctx copilot.CommandContext) error {
			calledArgs = ctx.Args
			return nil
		}},
	}

	handled, _, _ := chat.ExportTryDispatchSlashCommand(m, "/mode plan")

	assert.True(t, handled)
	assert.Equal(t, "plan", calledArgs)
	assert.NoError(t, chat.ExportGetErr(m))
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

	m := chat.NewModel(newCommandPickerTestParams())
	setTestCommands(m)

	handled, _, _ := chat.ExportTryDispatchSlashCommand(m, "hello world")

	assert.False(t, handled)
}

func TestTryDispatchSlashCommand_CaseInsensitive(t *testing.T) {
	t.Parallel()

	called := false

	m := chat.NewModel(newCommandPickerTestParams())
	config := chat.ExportGetSessionConfig(m)
	config.Commands = []copilot.CommandDefinition{
		{Name: "mode", Handler: func(_ copilot.CommandContext) error {
			called = true
			return nil
		}},
	}

	handled, _, _ := chat.ExportTryDispatchSlashCommand(m, "/MODE plan")

	assert.True(t, handled)
	assert.True(t, called)
}

// --- Option picker tests ---

func TestOptionPicker_ShowsOnCommandWithSpace(t *testing.T) {
	t.Parallel()

	m := chat.NewModel(newCommandPickerTestParams())
	setTestCommands(m)

	chat.ExportSetTextareaValue(m, "/mode ")
	chat.ExportUpdateCommandPicker(m)

	assert.False(t, chat.ExportShowCommandPicker(m))
	assert.True(t, chat.ExportShowOptionPicker(m))
	assert.Equal(t, "mode", chat.ExportActiveCommandName(m))
	assert.Equal(t, 3, len(chat.ExportFilteredOptions(m))) // interactive, plan, autopilot
}

func TestOptionPicker_FiltersOnPartialArg(t *testing.T) {
	t.Parallel()

	m := chat.NewModel(newCommandPickerTestParams())
	setTestCommands(m)

	chat.ExportSetTextareaValue(m, "/mode pl")
	chat.ExportUpdateCommandPicker(m)

	assert.True(t, chat.ExportShowOptionPicker(m))

	filtered := chat.ExportFilteredOptions(m)
	assert.Equal(t, 1, len(filtered))
	assert.Equal(t, "plan", filtered[0].Name)
}

func TestOptionPicker_HidesForCommandWithoutOptions(t *testing.T) {
	t.Parallel()

	m := chat.NewModel(newCommandPickerTestParams())
	setTestCommands(m)

	chat.ExportSetTextareaValue(m, "/clear ")
	chat.ExportUpdateCommandPicker(m)

	assert.False(t, chat.ExportShowCommandPicker(m))
	assert.False(t, chat.ExportShowOptionPicker(m))
}

func TestOptionPicker_ClampsIndex(t *testing.T) {
	t.Parallel()

	m := chat.NewModel(newCommandPickerTestParams())
	setTestCommands(m)

	// Show all options
	chat.ExportSetTextareaValue(m, "/mode ")
	chat.ExportUpdateCommandPicker(m)

	// Set index beyond what filter will produce
	chat.ExportSetOptionPickerIndex(m, 2)

	// Now filter to single option
	chat.ExportSetTextareaValue(m, "/mode pl")
	chat.ExportUpdateCommandPicker(m)

	assert.True(t, chat.ExportShowOptionPicker(m))
	assert.Equal(t, 0, chat.ExportOptionPickerIndex(m))
}

func TestOptionPicker_CaseInsensitive(t *testing.T) {
	t.Parallel()

	m := chat.NewModel(newCommandPickerTestParams())
	setTestCommands(m)

	chat.ExportSetTextareaValue(m, "/mode PL")
	chat.ExportUpdateCommandPicker(m)

	assert.True(t, chat.ExportShowOptionPicker(m))
	assert.Equal(t, 1, len(chat.ExportFilteredOptions(m)))
}

// --- pickerExtraHeight tests ---

func TestPickerExtraHeight_OptionPicker(t *testing.T) {
	t.Parallel()

	m := chat.NewModel(newCommandPickerTestParams())
	setTestCommands(m)

	chat.ExportSetTextareaValue(m, "/mode ")
	chat.ExportUpdateCommandPicker(m)

	// 3 options + 2 for border
	assert.Equal(t, 5, chat.ExportPickerExtraHeight(m))
}
