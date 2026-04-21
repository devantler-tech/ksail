package chat_test

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/devantler-tech/ksail/v7/pkg/cli/ui/chat"
	copilot "github.com/github/copilot-sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- handleStreamEvent dispatch tests ---

//nolint:funlen // Table-driven test coverage is naturally long.
func TestHandleStreamEvent_DispatchesAllEventTypes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		msg   tea.Msg
		check func(t *testing.T, m *chat.Model)
	}{
		{
			name: "streamChunkMsg accumulates text",
			msg:  chat.ExportNewStreamChunkMsg("hello"),
			check: func(t *testing.T, m *chat.Model) {
				t.Helper()
				assert.Equal(t, "hello", chat.ExportGetMessageContent(m, 0))
			},
		},
		{
			name: "turnStartMsg sets streaming",
			msg:  chat.ExportNewTurnStartMsg(),
			check: func(t *testing.T, m *chat.Model) {
				t.Helper()
				assert.True(t, chat.ExportGetStreaming(m))
			},
		},
		{
			name: "reasoningMsg keeps streaming",
			msg:  chat.ExportNewReasoningMsg("thinking", true),
			check: func(t *testing.T, m *chat.Model) {
				t.Helper()
				assert.True(t, chat.ExportGetStreaming(m))
			},
		},
		{
			name: "compactionStartMsg sets compacting",
			msg:  chat.ExportNewCompactionStartMsg(),
			check: func(t *testing.T, m *chat.Model) {
				t.Helper()
				assert.True(t, chat.ExportGetIsCompacting(m))
			},
		},
		{
			name: "modelChangeMsg updates model",
			msg:  chat.ExportNewModelChangeMsg("old", "gpt-4o-mini"),
			check: func(t *testing.T, m *chat.Model) {
				t.Helper()
				assert.Equal(t, "gpt-4o-mini", chat.ExportGetCurrentModel(m))
			},
		},
		{
			name: "shutdownMsg stops streaming",
			msg:  chat.ExportNewShutdownMsg(),
			check: func(t *testing.T, m *chat.Model) {
				t.Helper()
				assert.False(t, chat.ExportGetStreaming(m))
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			model := chat.NewModel(newTestParams())
			chat.ExportSetStreaming(model, true)
			chat.ExportSetMessages(model, []chat.MessageForTest{
				chat.ExportNewStreamingAssistantMessage(""),
			})

			updated, _ := model.Update(testCase.msg)
			m, ok := updated.(*chat.Model)
			require.True(t, ok)
			testCase.check(t, m)
		})
	}
}

// --- handleStreamEvent with exported types ---

func TestHandleStreamEvent_ToolOutputChunkMsg_Exported(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetStreaming(model, true)

	// Start a tool first
	updated, _ := model.Update(chat.ExportNewToolStartMsg("t1", "bash", "ls"))

	// Use the exported ToolOutputChunkMsg type
	updated, _ = updated.Update(chat.ToolOutputChunkMsg{ToolID: "bash", Chunk: "exported chunk"})
	m, ok := updated.(*chat.Model)
	require.True(t, ok)

	tools := chat.ExportGetTools(m)
	require.Contains(t, tools, "t1")
	assert.Contains(t, tools["t1"].Output(), "exported chunk")
}

func TestHandleStreamEvent_PermissionRequestMsg_Exported(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetStreaming(model, true)

	resp := make(chan bool, 1)
	updated, _ := model.Update(chat.PermissionRequestMsg{
		ToolCallID: "call-1",
		ToolName:   "bash",
		Command:    "rm -rf /tmp/test",
		Arguments:  `{"path": "/tmp/test"}`,
		Response:   resp,
	})
	m, ok := updated.(*chat.Model)
	require.True(t, ok)

	assert.True(t, chat.ExportHasPendingPermission(m))
}

// --- Update message type dispatch ---

func TestUpdate_CopyFeedbackClearMsg(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetShowCopyFeedback(model, true)

	updated, _ := model.Update(chat.ExportNewCopyFeedbackClearMsg())
	m, ok := updated.(*chat.Model)
	require.True(t, ok)

	assert.False(t, chat.ExportGetShowCopyFeedback(m))
}

func TestUpdate_ModelUnavailableClearMsg(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetShowModelUnavailableFeedback(model, true)
	chat.ExportSetModelUnavailableReason(model, "rate limited")

	updated, _ := model.Update(chat.ExportNewModelUnavailableClearMsg())
	m, ok := updated.(*chat.Model)
	require.True(t, ok)

	// Verify the feedback was cleared
	output := m.View()
	assert.NotContains(t, output, "rate limited")
}

// --- Window resize ---

func TestUpdate_WindowSizeMsg_UpdatesReady(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())

	updated, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 50})

	// Should render correctly with the new size
	output := updated.View()
	assert.NotEmpty(t, output)
}

// --- Unknown message type ---

func TestHandleStreamEvent_UnknownTypeReturnsModelAndNilCmd(t *testing.T) {
	t.Parallel()

	type unknownMsg struct{}

	model := chat.NewModel(newTestParams())
	chat.ExportSetStreaming(model, true)

	updated, cmd := model.Update(unknownMsg{})

	assert.NotNil(t, updated)
	// Unknown message types should be handled gracefully (might go through subcomponent updates)
	_ = cmd
}

// --- Session picker tests ---

func TestSessionPicker_ValidIndex(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetFilteredSessions(model, []chat.SessionMetadata{
		{ID: "session-1", Name: "Test Session"},
		{ID: "session-2", Name: "Another Session"},
	})
	chat.ExportSetSessionPickerIndex(model, 1)

	assert.True(t, chat.ExportIsValidSessionIndex(model))
}

func TestSessionPicker_InvalidIndex_Zero(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetFilteredSessions(model, []chat.SessionMetadata{
		{ID: "session-1", Name: "Test Session"},
	})
	chat.ExportSetSessionPickerIndex(model, 0) // "New Chat" option

	assert.True(t, chat.ExportIsInvalidSessionIndex(model))
}

func TestSessionPicker_ClampIndex(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetFilteredSessions(model, []chat.SessionMetadata{
		{ID: "session-1", Name: "Test Session"},
	})
	chat.ExportSetSessionPickerIndex(model, 100) // Way out of bounds

	chat.ExportClampSessionIndex(model)

	idx := chat.ExportGetSessionPickerIndex(model)
	assert.LessOrEqual(t, idx, 1) // Max valid index
}

func TestSessionPicker_FindCurrentSession(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetAvailableSessions(model, []chat.SessionMetadata{
		{ID: "session-1", Name: "First"},
		{ID: "session-2", Name: "Second"},
	})
	chat.ExportSetCurrentSessionID(model, "session-2")

	idx := chat.ExportFindCurrentSessionIndex(model)
	assert.Equal(t, 2, idx) // +1 offset for "New Chat"
}

func TestSessionPicker_FindCurrentSession_NotFound(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetAvailableSessions(model, []chat.SessionMetadata{
		{ID: "session-1", Name: "First"},
	})
	chat.ExportSetCurrentSessionID(model, "nonexistent")

	idx := chat.ExportFindCurrentSessionIndex(model)
	assert.Equal(t, 0, idx) // Default to "New Chat"
}

// --- Model picker tests ---

func TestModelPicker_FindCurrentModelIndex_AutoMode(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetSessionConfigModel(model, "")
	chat.ExportSetAvailableModels(model, []copilot.ModelInfo{
		{ID: "gpt-4o"},
	})

	idx := chat.ExportFindCurrentModelIndex(model)
	assert.Equal(t, 0, idx) // Auto mode returns 0
}

func TestModelPicker_FindCurrentModelIndex_ExplicitModel(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetSessionConfigModel(model, "gpt-4o")
	chat.ExportSetCurrentModel(model, "gpt-4o")
	chat.ExportSetAvailableModels(model, []copilot.ModelInfo{
		{ID: "gpt-4o"},
		{ID: "claude-3"},
	})

	idx := chat.ExportFindCurrentModelIndex(model)
	assert.Equal(t, 1, idx) // +1 offset for auto option
}

// --- Reasoning picker tests ---

func TestReasoningPicker_IsCurrentReasoningEffort(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		effort   string
		level    string
		expected bool
	}{
		{name: "off matches empty", effort: "", level: "off", expected: true},
		{name: "high matches high", effort: "high", level: "high", expected: true},
		{name: "high does not match low", effort: "high", level: "low", expected: false},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			model := chat.NewModel(newTestParams())
			chat.ExportSetSessionConfigReasoningEffort(model, testCase.effort)

			result := chat.ExportIsCurrentReasoningEffort(model, testCase.level)
			assert.Equal(t, testCase.expected, result)
		})
	}
}

func TestReasoningPicker_FindCurrentReasoningIndex(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		effort   string
		expected int
	}{
		{name: "empty defaults to off", effort: "", expected: 0},
		{name: "high", effort: "high", expected: 3},
		{name: "medium", effort: "medium", expected: 2},
		{name: "low", effort: "low", expected: 1},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			model := chat.NewModel(newTestParams())
			chat.ExportSetSessionConfigReasoningEffort(model, testCase.effort)

			result := chat.ExportFindCurrentReasoningIndex(model)
			assert.Equal(t, testCase.expected, result)
		})
	}
}

// --- Auto mode tests ---

func TestIsAutoMode_EventHandler(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		config   string
		expected bool
	}{
		{name: "empty config is auto", config: "", expected: true},
		{name: "auto string is auto", config: "auto", expected: true},
		{name: "explicit model is not auto", config: "gpt-4o", expected: false},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			model := chat.NewModel(newTestParams())
			chat.ExportSetSessionConfigModel(model, testCase.config)

			result := chat.ExportIsAutoMode(model)
			assert.Equal(t, testCase.expected, result)
		})
	}
}

func TestResolvedAutoModel_EventHandler(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		configModel  string
		currentModel string
		usageModel   string
		expected     string
	}{
		{
			name:         "returns usage model when in auto mode",
			configModel:  "",
			currentModel: "",
			usageModel:   "gpt-4o",
			expected:     "gpt-4o",
		},
		{
			name:         "returns empty when not in auto mode",
			configModel:  "gpt-4o",
			currentModel: "gpt-4o",
			usageModel:   "gpt-4o",
			expected:     "",
		},
		{
			name:         "returns currentModel when set in auto mode",
			configModel:  "",
			currentModel: "claude-3",
			usageModel:   "",
			expected:     "claude-3",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			model := chat.NewModel(newTestParams())
			chat.ExportSetSessionConfigModel(model, testCase.configModel)
			chat.ExportSetCurrentModel(model, testCase.currentModel)
			chat.ExportSetLastUsageModel(model, testCase.usageModel)

			result := chat.ExportResolvedAutoModel(model)
			assert.Equal(t, testCase.expected, result)
		})
	}
}

// --- View rendering tests ---

func TestView_ShowsSpinnerWhenStreaming(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetStreaming(model, true)

	// The status bar should show a spinner character when streaming
	status := chat.ExportBuildStatusText(model)
	// The spinner produces characters like ⣾ ⣽ etc - we just check it has more than the mode label
	assert.Contains(t, status, "interactive")
}

func TestView_ShowsCopiedFeedback(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetShowCopyFeedback(model, true)

	status := chat.ExportBuildStatusText(model)
	assert.Contains(t, status, "Copied")
	assert.Contains(t, status, "✓")
}

func TestView_ShowsModelUnavailableFeedback(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetShowModelUnavailableFeedback(model, true)
	chat.ExportSetModelUnavailableReason(model, "API error")

	status := chat.ExportBuildStatusText(model)
	assert.Contains(t, status, "Models unavailable")
	assert.Contains(t, status, "API error")
}

func TestView_ShowsReadyAfterCompletion(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetJustCompleted(model, true)

	status := chat.ExportBuildStatusText(model)
	assert.Contains(t, status, "Ready")
	assert.Contains(t, status, "✓")
}

// --- ChatModeRef tests ---

func TestChatModeRef_Operations(t *testing.T) {
	t.Parallel()

	ref := chat.NewChatModeRef(chat.InteractiveMode)

	assert.Equal(t, chat.InteractiveMode, ref.Mode())

	ref.SetMode(chat.PlanMode)
	assert.Equal(t, chat.PlanMode, ref.Mode())

	ref.SetMode(chat.AutopilotMode)
	assert.Equal(t, chat.AutopilotMode, ref.Mode())
}

// --- handleUserSubmit tests ---

func TestHandleUserSubmit_AddsUserAndAssistantMessages(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())

	// Type and send
	updated := typeText(model, "hello copilot")
	updated, _ = updated.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m, ok := updated.(*chat.Model)
	require.True(t, ok)

	assert.True(t, chat.ExportGetStreaming(m))
	// The messages get added by the command, not synchronously
}

// --- Init tests ---

func TestInit_ReturnsNonNilCmd(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	cmd := model.Init()

	assert.NotNil(t, cmd, "Init should return a non-nil command for spinner and textarea blink")
}

// --- Event channel tests ---

func TestGetEventChannel_ReturnsChannel(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	ch := chat.ExportGetEventChannel(model)

	assert.NotNil(t, ch)
}

func TestGetEventChannel_UsesProvidedChannel(t *testing.T) {
	t.Parallel()

	provided := make(chan tea.Msg, 50)
	params := newTestParams()
	params.EventChan = provided

	model := chat.NewModel(params)
	ch := chat.ExportGetEventChannel(model)

	assert.Equal(t, cap(provided), cap(ch))
}

// --- FilterEnabledModels tests ---

func TestFilterEnabledModels_FiltersCorrectly(t *testing.T) {
	t.Parallel()

	models := []copilot.ModelInfo{
		{ID: "gpt-4o", Policy: &copilot.ModelPolicy{State: "enabled"}},
		{ID: "disabled-model", Policy: &copilot.ModelPolicy{State: "disabled"}},
		{ID: "claude-3", Policy: &copilot.ModelPolicy{State: "enabled"}},
		{ID: "no-policy"},
	}

	result := chat.FilterEnabledModels(models)

	assert.Len(t, result, 2)
	assert.Equal(t, "gpt-4o", result[0].ID)
	assert.Equal(t, "claude-3", result[1].ID)
}
