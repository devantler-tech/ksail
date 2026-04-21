package chat_test

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/devantler-tech/ksail/v7/pkg/cli/ui/chat"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- ModeRef concurrent safety ---

func TestModeRef_ConcurrentAccess(t *testing.T) {
	t.Parallel()

	ref := chat.NewModeRef(false)
	done := make(chan struct{})

	go func() {
		for range 100 {
			ref.SetEnabled(true)
			ref.SetEnabled(false)
		}

		close(done)
	}()

	for range 100 {
		_ = ref.IsEnabled()
	}

	<-done
}

// --- ChatModeRef concurrent safety ---

func TestChatModeRef_ConcurrentAccess(t *testing.T) {
	t.Parallel()

	ref := chat.NewChatModeRef(chat.InteractiveMode)
	done := make(chan struct{})

	go func() {
		for range 100 {
			ref.SetMode(chat.PlanMode)
			ref.SetMode(chat.AutopilotMode)
			ref.SetMode(chat.InteractiveMode)
		}

		close(done)
	}()

	for range 100 {
		_ = ref.Mode()
	}

	<-done
}

// --- View with messages ---

func TestView_WithMixedMessages(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetMessages(model, []chat.MessageForTest{
		chat.ExportNewUserMessage("first question"),
		chat.ExportNewAssistantMessage("first answer"),
		chat.ExportNewUserMessage("second question"),
		chat.ExportNewAssistantMessage("second answer"),
	})

	var updated tea.Model = model

	updated, _ = updated.Update(tea.WindowSizeMsg{Width: 100, Height: 40})

	output := updated.View()

	assert.Contains(t, output, "first question")
	assert.Contains(t, output, "first answer")
	assert.Contains(t, output, "second question")
	assert.Contains(t, output, "second answer")
}

func TestView_WithAssistantToolsInMessage(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())

	tool := chat.ExportNewToolExecutionFull(
		"bash",
		chat.ToolStatusComplete,
		true,
		"> ls",
		"file1\nfile2",
	)
	msg := chat.ExportNewAssistantMessageWithTools(
		"Here are the files:",
		[]*chat.ToolExecutionForTest{tool},
	)

	chat.ExportSetMessages(model, []chat.MessageForTest{msg})

	var updated tea.Model = model

	updated, _ = updated.Update(tea.WindowSizeMsg{Width: 100, Height: 40})

	output := updated.View()

	assert.Contains(t, output, "file1")
	assert.Contains(t, output, "✓")
}

// --- Pending prompt queue management ---

func TestPendingPromptQueue_PeekAndDrop(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetStreaming(model, true)

	updated := typeText(model, "queued")
	updated, _ = updated.Update(ctrlQKey())

	modelState, isModel := updated.(*chat.Model)
	require.True(t, isModel)

	assert.True(t, chat.ExportPeekNextPendingPrompt(modelState))

	chat.ExportDropNextPendingPrompt(modelState)

	assert.False(t, chat.ExportPeekNextPendingPrompt(modelState))
}

// --- Tool toggle with message tools ---

func TestToggleTools_WithMessageTools(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())

	tool := chat.ExportNewToolExecutionFull("bash", chat.ToolStatusComplete, true, "ls", "output")
	msg := chat.ExportNewAssistantMessageWithTools("content", []*chat.ToolExecutionForTest{tool})
	chat.ExportSetMessages(model, []chat.MessageForTest{msg})

	updated, _ := model.Update(tea.KeyMsg{Type: tea.KeyCtrlT})

	output := updated.View()
	_ = output
}

// --- View with long tool output ---

func TestView_ToolOutputWrapping(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetStreaming(model, true)

	chat.ExportSetMessages(model, []chat.MessageForTest{
		chat.ExportNewStreamingAssistantMessage("response"),
	})

	var updated tea.Model = model

	updated, _ = updated.Update(chat.ExportNewToolStartMsg("t1", "bash", "ls"))

	longOutput := strings.Repeat("abcdefghij", 20)
	updated, _ = updated.Update(chat.ExportNewToolOutputChunkMsg("bash", longOutput))

	updated, _ = updated.Update(tea.WindowSizeMsg{Width: 80, Height: 30})

	output := updated.View()
	assert.NotEmpty(t, output)
}

// --- View with tool position interleaving ---

func TestView_ToolPositionInterleaving(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())

	tool := chat.ExportNewToolExecutionWithPosition(
		"bash",
		chat.ToolStatusComplete,
		true,
		"ls",
		"output",
		5,
	)
	msg := chat.ExportNewAssistantMessageWithTools(
		"Hello world",
		[]*chat.ToolExecutionForTest{tool},
	)
	chat.ExportSetMessages(model, []chat.MessageForTest{msg})

	var updated tea.Model = model

	updated, _ = updated.Update(tea.WindowSizeMsg{Width: 100, Height: 40})

	output := updated.View()
	assert.Contains(t, output, "Hello")
	assert.Contains(t, output, "output")
}

// --- defaultCommandBuilders tests ---

func TestDefaultCommandBuilders_ClusterInfo(t *testing.T) {
	t.Parallel()

	builders := chat.DefaultToolDisplayConfig().CommandBuilders

	result := chat.ExportExtractCommandFromArgs("ksail_cluster_info",
		map[string]any{"name": "my-cluster"}, builders)

	assert.Equal(t, "ksail cluster info --name my-cluster", result)
}

func TestDefaultCommandBuilders_WorkloadGet(t *testing.T) {
	t.Parallel()

	builders := chat.DefaultToolDisplayConfig().CommandBuilders

	tests := []struct {
		name     string
		args     map[string]any
		expected string
	}{
		{
			name:     "basic resource",
			args:     map[string]any{"resource": "pods"},
			expected: "ksail workload get pods",
		},
		{
			name:     "with name and namespace",
			args:     map[string]any{"resource": "pods", "name": "nginx", "namespace": "default"},
			expected: "ksail workload get pods nginx -n default",
		},
		{
			name:     "all namespaces",
			args:     map[string]any{"resource": "pods", "all_namespaces": true},
			expected: "ksail workload get pods -A",
		},
		{
			name:     "with output format",
			args:     map[string]any{"resource": "pods", "output": "yaml"},
			expected: "ksail workload get pods -o yaml",
		},
		{
			name:     "no resource returns empty",
			args:     map[string]any{},
			expected: "",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := chat.ExportExtractCommandFromArgs(
				"ksail_workload_get",
				testCase.args,
				builders,
			)
			assert.Equal(t, testCase.expected, result)
		})
	}
}

func TestDefaultCommandBuilders_ListDir(t *testing.T) {
	t.Parallel()

	builders := chat.DefaultToolDisplayConfig().CommandBuilders

	tests := []struct {
		name     string
		toolName string
		args     map[string]any
		expected string
	}{
		{
			name:     "list_dir with path",
			toolName: "list_dir",
			args:     map[string]any{"path": "/etc"},
			expected: "ls /etc",
		},
		{
			name:     "list_dir without path",
			toolName: "list_dir",
			args:     map[string]any{},
			expected: "ls .",
		},
		{
			name:     "list_directory alias",
			toolName: "list_directory",
			args:     map[string]any{"path": "/home"},
			expected: "ls /home",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result := chat.ExportExtractCommandFromArgs(testCase.toolName, testCase.args, builders)
			assert.Equal(t, testCase.expected, result)
		})
	}
}

func TestDefaultCommandBuilders_ReadFile_Empty(t *testing.T) {
	t.Parallel()

	builders := chat.DefaultToolDisplayConfig().CommandBuilders

	result := chat.ExportExtractCommandFromArgs("read_file", map[string]any{}, builders)

	assert.Empty(t, result)
}

// --- DefaultToolDisplayConfig tests ---

func TestDefaultToolDisplayConfig_HasMappings(t *testing.T) {
	t.Parallel()

	config := chat.DefaultToolDisplayConfig()

	assert.NotEmpty(t, config.NameMappings)
	assert.Contains(t, config.NameMappings, "bash")
	assert.Contains(t, config.NameMappings, "read_file")
	assert.Contains(t, config.NameMappings, "ksail_cluster_list")

	assert.NotEmpty(t, config.CommandBuilders)
}

// --- DefaultThemeConfig tests ---

func TestDefaultThemeConfig_HasRequiredFields(t *testing.T) {
	t.Parallel()

	config := chat.DefaultThemeConfig()

	assert.NotNil(t, config.Logo)
	assert.NotNil(t, config.Tagline)
	assert.NotEmpty(t, config.AssistantLabel)
	assert.NotEmpty(t, config.Placeholder)
	assert.NotEmpty(t, config.WelcomeMessage)
	assert.NotEmpty(t, config.ExitMessage)
	assert.NotEmpty(t, config.GoodbyeMessage)
	assert.NotEmpty(t, config.SessionDir)
	assert.NotEmpty(t, config.Logo())
	assert.NotEmpty(t, config.Tagline())
}

// --- View status indicators ---

func TestStatusBar_CopiedFeedback(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetShowCopyFeedback(model, true)

	status := chat.ExportBuildStatusText(model)
	assert.Contains(t, status, "Copied")
	assert.Contains(t, status, "✓")
}

func TestStatusBar_ModelUnavailableFeedback_WithReason(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetShowModelUnavailableFeedback(model, true)
	chat.ExportSetModelUnavailableReason(model, "API error")

	status := chat.ExportBuildStatusText(model)
	assert.Contains(t, status, "Models unavailable")
	assert.Contains(t, status, "API error")
}

func TestStatusBar_ReadyAfterCompletion(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetJustCompleted(model, true)

	status := chat.ExportBuildStatusText(model)
	assert.Contains(t, status, "Ready")
	assert.Contains(t, status, "✓")
}

// --- Event channel tests ---

func TestEventChannel_UsesProvidedChannel(t *testing.T) {
	t.Parallel()

	provided := make(chan tea.Msg, 50)
	params := newTestParams()
	params.EventChan = provided

	model := chat.NewModel(params)
	ch := chat.ExportGetEventChannel(model)

	assert.Equal(t, cap(provided), cap(ch))
}
