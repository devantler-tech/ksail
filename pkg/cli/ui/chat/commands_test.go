package chat_test

import (
	"bytes"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/devantler-tech/ksail/v7/pkg/cli/ui/chat"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testModeCmd = "mode"

// --- ParseChatMode tests ---

func TestParseChatMode_ValidModes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input    string
		expected chat.ChatMode
	}{
		{"interactive", chat.InteractiveMode},
		{"plan", chat.PlanMode},
		{"autopilot", chat.AutopilotMode},
		{"INTERACTIVE", chat.InteractiveMode},
		{"Plan", chat.PlanMode},
		{"AUTOPILOT", chat.AutopilotMode},
		{"  plan  ", chat.PlanMode},
	}

	for _, testCase := range tests {
		t.Run(testCase.input, func(t *testing.T) {
			t.Parallel()

			mode, ok := chat.ParseChatMode(testCase.input)
			assert.True(t, ok, "ParseChatMode(%q) should succeed", testCase.input)
			assert.Equal(t, testCase.expected, mode)
		})
	}
}

func TestParseChatMode_InvalidModes(t *testing.T) {
	t.Parallel()

	tests := []string{"", "invalid", "auto", "manual", "planning"}

	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			t.Parallel()

			_, ok := chat.ParseChatMode(input)
			assert.False(t, ok, "ParseChatMode(%q) should fail", input)
		})
	}
}

// --- BuildTUISlashCommands tests ---

func TestBuildTUISlashCommands_ReturnsExpectedCommands(t *testing.T) {
	t.Parallel()

	eventChan := make(chan tea.Msg, 10)
	commands := chat.BuildTUISlashCommands(eventChan)

	expectedNames := []string{"mode", "model", "new", "sessions", "help", "clear"}
	actualNames := make([]string, len(commands))

	for i, cmd := range commands {
		actualNames[i] = cmd.Name
	}

	assert.Equal(t, expectedNames, actualNames)

	for _, cmd := range commands {
		assert.NotEmpty(t, cmd.Description)
		assert.NotNil(t, cmd.Handler)
	}
}

func TestBuildTUISlashCommands_ClearSendsMessage(t *testing.T) {
	t.Parallel()

	eventChan := make(chan tea.Msg, 10)
	commands := chat.BuildTUISlashCommands(eventChan)

	// Find /clear command
	var clearCmd *chat.ExportCommandDefinition

	for i := range commands {
		if commands[i].Name == "clear" {
			clearCmd = &chat.ExportCommandDefinition{Def: commands[i]}

			break
		}
	}

	require.NotNil(t, clearCmd, "/clear command not found")

	err := clearCmd.Def.Handler(chat.ExportCommandContext{})
	require.NoError(t, err)

	msg := <-eventChan
	_, ok := msg.(chat.ExportClearViewportMsg)
	assert.True(t, ok, "expected clearViewportMsg, got %T", msg)
}

func TestBuildTUISlashCommands_HelpSendsMessage(t *testing.T) {
	t.Parallel()

	eventChan := make(chan tea.Msg, 10)
	commands := chat.BuildTUISlashCommands(eventChan)

	var helpCmd *chat.ExportCommandDefinition

	for i := range commands {
		if commands[i].Name == "help" {
			helpCmd = &chat.ExportCommandDefinition{Def: commands[i]}

			break
		}
	}

	require.NotNil(t, helpCmd, "/help command not found")

	err := helpCmd.Def.Handler(chat.ExportCommandContext{})
	require.NoError(t, err)

	msg := <-eventChan
	_, ok := msg.(chat.ExportShowHelpMsg)
	assert.True(t, ok, "expected showHelpMsg, got %T", msg)
}

func TestBuildTUISlashCommands_ModeSendsMessage(t *testing.T) {
	t.Parallel()

	eventChan := make(chan tea.Msg, 10)
	commands := chat.BuildTUISlashCommands(eventChan)

	var modeCmd *chat.ExportCommandDefinition

	for i := range commands {
		if commands[i].Name == testModeCmd {
			modeCmd = &chat.ExportCommandDefinition{Def: commands[i]}

			break
		}
	}

	require.NotNil(t, modeCmd, "/mode command not found")

	err := modeCmd.Def.Handler(chat.ExportCommandContextWithArgs("plan"))
	require.NoError(t, err)

	msg := <-eventChan
	modeMsg, ok := msg.(chat.ExportModeChangeRequestMsg)
	assert.True(t, ok, "expected modeChangeRequestMsg, got %T", msg)
	assert.Equal(t, chat.PlanMode, modeMsg.Mode)
}

func TestBuildTUISlashCommands_ModeEmptyArgsReturnsError(t *testing.T) {
	t.Parallel()

	eventChan := make(chan tea.Msg, 10)
	commands := chat.BuildTUISlashCommands(eventChan)

	var modeCmd *chat.ExportCommandDefinition

	for i := range commands {
		if commands[i].Name == testModeCmd {
			modeCmd = &chat.ExportCommandDefinition{Def: commands[i]}

			break
		}
	}

	require.NotNil(t, modeCmd, "/mode command not found")

	err := modeCmd.Def.Handler(chat.ExportCommandContext{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "usage")
}

func TestBuildTUISlashCommands_ModeInvalidReturnsError(t *testing.T) {
	t.Parallel()

	eventChan := make(chan tea.Msg, 10)
	commands := chat.BuildTUISlashCommands(eventChan)

	var modeCmd *chat.ExportCommandDefinition

	for i := range commands {
		if commands[i].Name == testModeCmd {
			modeCmd = &chat.ExportCommandDefinition{Def: commands[i]}

			break
		}
	}

	require.NotNil(t, modeCmd, "/mode command not found")

	err := modeCmd.Def.Handler(chat.ExportCommandContextWithArgs("badmode"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid mode")
}

func TestBuildTUISlashCommands_ModelNoArgsSendsPicker(t *testing.T) {
	t.Parallel()

	eventChan := make(chan tea.Msg, 10)
	commands := chat.BuildTUISlashCommands(eventChan)

	var modelCmd *chat.ExportCommandDefinition

	for i := range commands {
		if commands[i].Name == "model" {
			modelCmd = &chat.ExportCommandDefinition{Def: commands[i]}

			break
		}
	}

	require.NotNil(t, modelCmd, "/model command not found")

	err := modelCmd.Def.Handler(chat.ExportCommandContext{})
	require.NoError(t, err)

	msg := <-eventChan
	_, ok := msg.(chat.ExportOpenModelPickerMsg)
	assert.True(t, ok, "expected openModelPickerMsg, got %T", msg)
}

func TestBuildTUISlashCommands_ModelWithArgsSendsSetRequest(t *testing.T) {
	t.Parallel()

	eventChan := make(chan tea.Msg, 10)
	commands := chat.BuildTUISlashCommands(eventChan)

	var modelCmd *chat.ExportCommandDefinition

	for i := range commands {
		if commands[i].Name == "model" {
			modelCmd = &chat.ExportCommandDefinition{Def: commands[i]}

			break
		}
	}

	require.NotNil(t, modelCmd, "/model command not found")

	err := modelCmd.Def.Handler(chat.ExportCommandContextWithArgs("gpt-5"))
	require.NoError(t, err)

	msg := <-eventChan
	setMsg, ok := msg.(chat.ExportModelSetRequestMsg)
	assert.True(t, ok, "expected modelSetRequestMsg, got %T", msg)
	assert.Equal(t, "gpt-5", setMsg.Model)
}

// --- BuildNonTUISlashCommands tests ---

func TestBuildNonTUISlashCommands_ReturnsExpectedCommands(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	commands := chat.BuildNonTUISlashCommands(&buf)

	assert.NotEmpty(t, commands)

	for _, cmd := range commands {
		assert.NotEmpty(t, cmd.Name)
		assert.NotEmpty(t, cmd.Description)
		assert.NotNil(t, cmd.Handler)
	}
}

func TestBuildNonTUISlashCommands_HelpPrintsOutput(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	commands := chat.BuildNonTUISlashCommands(&buf)

	var helpCmd *chat.ExportCommandDefinition

	for i := range commands {
		if commands[i].Name == "help" {
			helpCmd = &chat.ExportCommandDefinition{Def: commands[i]}

			break
		}
	}

	require.NotNil(t, helpCmd, "/help command not found")

	err := helpCmd.Def.Handler(chat.ExportCommandContext{})
	require.NoError(t, err)
	assert.Contains(t, buf.String(), "Available commands")
}
