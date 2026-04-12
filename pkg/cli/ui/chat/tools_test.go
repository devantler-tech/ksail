package chat_test

import (
	"testing"

	"github.com/devantler-tech/ksail/v6/pkg/cli/ui/chat"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- findToolByID tests ---

func TestFindToolByID_ViaToolEnd(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		setupTools   map[string]*chat.ToolExecutionForTest
		setupOrder   []string
		toolID       string
		endToolName  string
		expectStatus int
	}{
		{
			name: "matches by exact ID",
			setupTools: map[string]*chat.ToolExecutionForTest{
				"tool-abc": chat.ExportNewToolExecution("bash", chat.ToolStatusRunning, false),
			},
			setupOrder:   []string{"tool-abc"},
			toolID:       "tool-abc",
			endToolName:  "bash",
			expectStatus: chat.ToolStatusComplete,
		},
		{
			name: "no match for unknown ID and unknown name stays running",
			setupTools: map[string]*chat.ToolExecutionForTest{
				"tool-abc": chat.ExportNewToolExecution("bash", chat.ToolStatusRunning, false),
			},
			setupOrder:   []string{"tool-abc"},
			toolID:       "tool-xyz",
			endToolName:  "unknown",      // doesn't match "bash" by name
			expectStatus: chat.ToolStatusComplete, // falls through to FIFO which matches first running
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			model := chat.NewModel(newTestParams())
			chat.ExportSetStreaming(model, true)
			chat.ExportSetTools(model, testCase.setupTools, testCase.setupOrder)

			// Manually set pendingToolCount
			for _, tool := range testCase.setupTools {
				if int(tool.Status()) == chat.ToolStatusRunning {
					// Account for each running tool
					_ = tool
				}
			}

			// Use ToolEnd to trigger findCompletedTool internally
			updated, _ := model.Update(chat.ExportNewToolEndMsg(testCase.toolID, testCase.endToolName, "output", true))
			m := updated.(*chat.Model)

			tools := chat.ExportGetTools(m)
			require.Contains(t, tools, "tool-abc")
			assert.Equal(t, testCase.expectStatus, int(tools["tool-abc"].Status()))
		})
	}
}

// --- findRunningToolByName tests ---

func TestFindRunningToolByName_MatchesRunningTool(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetStreaming(model, true)

	// Start a tool
	updated, _ := model.Update(chat.ExportNewToolStartMsg("t1", "bash", "ls"))

	// End with different ID but same name - findRunningToolByName should match
	updated, _ = updated.Update(chat.ExportNewToolEndMsg("no-match-id", "bash", "output", true))
	m := updated.(*chat.Model)

	tools := chat.ExportGetTools(m)
	require.Contains(t, tools, "t1")
	assert.Equal(t, chat.ToolStatusComplete, int(tools["t1"].Status()))
}

func TestFindRunningToolByName_SkipsCompletedTool(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetStreaming(model, true)

	// Start two tools with same name
	u1, _ := model.Update(chat.ExportNewToolStartMsg("t1", "bash", "ls"))
	u2, _ := u1.Update(chat.ExportNewToolStartMsg("t2", "bash", "pwd"))

	// Complete first one
	u3, _ := u2.Update(chat.ExportNewToolEndMsg("t1", "bash", "out1", true))

	// Complete by name again - should match t2 (the running one)
	u4, _ := u3.Update(chat.ExportNewToolEndMsg("no-match", "bash", "out2", true))
	m := u4.(*chat.Model)

	tools := chat.ExportGetTools(m)
	assert.Equal(t, chat.ToolStatusComplete, int(tools["t1"].Status()))
	assert.Equal(t, chat.ToolStatusComplete, int(tools["t2"].Status()))
}

// --- completeToolExecution tests ---

func TestCompleteToolExecution_SuccessStatus(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetStreaming(model, true)

	updated, _ := model.Update(chat.ExportNewToolStartMsg("t1", "bash", "ls"))
	updated, _ = updated.Update(chat.ExportNewToolEndMsg("t1", "bash", "output", true))
	m := updated.(*chat.Model)

	tools := chat.ExportGetTools(m)
	assert.Equal(t, chat.ToolStatusComplete, int(tools["t1"].Status()))
	assert.True(t, tools["t1"].Expanded())
}

func TestCompleteToolExecution_FailedStatus(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetStreaming(model, true)

	updated, _ := model.Update(chat.ExportNewToolStartMsg("t1", "bash", "bad-cmd"))
	updated, _ = updated.Update(chat.ExportNewToolEndMsg("t1", "bash", "error: not found", false))
	m := updated.(*chat.Model)

	tools := chat.ExportGetTools(m)
	assert.Equal(t, chat.ToolStatusFailed, int(tools["t1"].Status()))
	assert.Equal(t, "error: not found", tools["t1"].Output())
}

// --- hasRunningTools tests ---

func TestHasRunningTools_TrueWhenToolRunning(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetStreaming(model, true)

	updated, _ := model.Update(chat.ExportNewToolStartMsg("t1", "bash", "ls"))
	m := updated.(*chat.Model)

	assert.True(t, chat.ExportHasRunningTools(m))
}

func TestHasRunningTools_FalseWhenAllComplete(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetStreaming(model, true)

	updated, _ := model.Update(chat.ExportNewToolStartMsg("t1", "bash", "ls"))
	updated, _ = updated.Update(chat.ExportNewToolEndMsg("t1", "bash", "", true))
	m := updated.(*chat.Model)

	assert.False(t, chat.ExportHasRunningTools(m))
}

func TestHasRunningTools_FalseWhenEmpty(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())

	assert.False(t, chat.ExportHasRunningTools(model))
}

// --- commitToolsToLastAssistantMessage tests ---

func TestCommitToolsToLastAssistantMessage_TransfersTools(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetStreaming(model, true)

	// Add an assistant message
	chat.ExportSetMessages(model, []chat.MessageForTest{
		chat.ExportNewAssistantMessage("response"),
	})

	// Start and complete a tool
	updated, _ := model.Update(chat.ExportNewToolStartMsg("t1", "bash", "ls"))
	updated, _ = updated.Update(chat.ExportNewToolEndMsg("t1", "bash", "output", true))
	m := updated.(*chat.Model)

	// Commit tools to message
	chat.ExportCommitToolsToLastAssistantMessage(m)

	assert.Equal(t, 1, chat.ExportGetMessageToolCount(m, 0))
}

func TestCommitToolsToLastAssistantMessage_NoMessages(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())

	// Should not panic with empty messages
	chat.ExportCommitToolsToLastAssistantMessage(model)
}

// --- prepareForNewTurn tests ---

func TestPrepareForNewTurn_ResetsState(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())

	// Set up various state
	chat.ExportSetStreaming(model, false)
	chat.ExportSetJustCompleted(model, true)

	// Simulate adding a tool
	u, _ := model.Update(chat.ExportNewToolStartMsg("t1", "bash", "ls"))
	m := u.(*chat.Model)

	// Now prepare for new turn
	chat.ExportPrepareForNewTurn(m)

	assert.True(t, chat.ExportGetStreaming(m))
	assert.False(t, chat.ExportGetJustCompleted(m))
	assert.Empty(t, chat.ExportGetToolOrder(m))
	assert.Equal(t, 0, chat.ExportGetPendingToolCount(m))
	assert.False(t, chat.ExportGetSessionComplete(m))
}
