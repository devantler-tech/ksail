package chat_test

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/devantler-tech/ksail/v7/pkg/cli/ui/chat"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	errStreamConnectionTimeout = errors.New("connection timeout")
	errStreamTest              = errors.New("test")
)

func requireModel(t *testing.T, updated tea.Model) *chat.Model {
	t.Helper()

	modelState, isModel := updated.(*chat.Model)
	require.True(t, isModel)

	return modelState
}

// --- handleStreamChunk tests ---

func TestHandleStreamChunk_AccumulatesText(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetStreaming(model, true)

	// Add a streaming assistant message placeholder
	chat.ExportSetMessages(model, []chat.MessageForTest{
		chat.ExportNewStreamingAssistantMessage(""),
	})

	var updated tea.Model = model

	// Send first chunk
	updated, _ = updated.Update(chat.ExportNewStreamChunkMsg("Hello"))
	modelState, isModel := updated.(*chat.Model)
	require.True(t, isModel)

	assert.Equal(t, "Hello", chat.ExportGetMessageContent(modelState, 0))

	// Send second chunk - should accumulate
	updated, _ = updated.Update(chat.ExportNewStreamChunkMsg(" world"))
	modelState, isModel = updated.(*chat.Model)
	require.True(t, isModel)

	assert.Equal(t, "Hello world", chat.ExportGetMessageContent(modelState, 0))
}

func TestHandleStreamChunk_EmptyContent(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetStreaming(model, true)

	chat.ExportSetMessages(model, []chat.MessageForTest{
		chat.ExportNewStreamingAssistantMessage("existing"),
	})

	var updated tea.Model = model

	// Send empty chunk - should not alter content
	updated, _ = updated.Update(chat.ExportNewStreamChunkMsg(""))
	modelState, isModel := updated.(*chat.Model)
	require.True(t, isModel)

	assert.Equal(t, "existing", chat.ExportGetMessageContent(modelState, 0))
}

func TestHandleStreamChunk_NoMessages(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetStreaming(model, true)
	chat.ExportSetMessages(model, nil)

	var updated tea.Model = model

	// Should not panic with empty messages
	updated, _ = updated.Update(chat.ExportNewStreamChunkMsg("text"))

	assert.NotNil(t, updated)
}

// --- handleAssistantMessage tests ---

func TestHandleAssistantMessage_OverridesPartialContent(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetStreaming(model, true)

	// Simulate partial content from streamed chunks
	chat.ExportSetMessages(model, []chat.MessageForTest{
		chat.ExportNewStreamingAssistantMessage("partial"),
	})

	var updated tea.Model = model

	// First send a chunk to populate currentResponse
	updated, _ = updated.Update(chat.ExportNewStreamChunkMsg("partial"))

	// Then send the final complete message which is longer
	updated, _ = updated.Update(chat.ExportNewAssistantMessageMsg("partial complete response"))
	modelState, isModel := updated.(*chat.Model)
	require.True(t, isModel)

	assert.Equal(t, "partial complete response", chat.ExportGetMessageContent(modelState, 0))
}

func TestHandleAssistantMessage_KeepsLongerCurrentResponse(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetStreaming(model, true)

	chat.ExportSetMessages(model, []chat.MessageForTest{
		chat.ExportNewStreamingAssistantMessage(""),
	})

	var updated tea.Model = model

	// Build up a longer response from chunks
	updated, _ = updated.Update(
		chat.ExportNewStreamChunkMsg("this is a very long response from chunks"),
	)

	// Final message is shorter (e.g., a summary) - currentResponse should be kept
	updated, _ = updated.Update(chat.ExportNewAssistantMessageMsg("short"))
	modelState, isModel := updated.(*chat.Model)
	require.True(t, isModel)

	assert.Equal(
		t,
		"this is a very long response from chunks",
		chat.ExportGetMessageContent(modelState, 0),
	)
}

// --- handleToolStart tests ---

func TestHandleToolStart_AddsToolExecution(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetStreaming(model, true)

	var updated tea.Model = model

	updated, _ = updated.Update(chat.ExportNewToolStartMsg("tool-1", "bash", "> ls"))
	modelState, isModel := updated.(*chat.Model)
	require.True(t, isModel)

	tools := chat.ExportGetTools(modelState)
	require.Contains(t, tools, "tool-1")
	assert.Equal(t, "bash", tools["tool-1"].Name())
	assert.Equal(t, 1, chat.ExportGetPendingToolCount(modelState))
	assert.Contains(t, chat.ExportGetToolOrder(modelState), "tool-1")
}

func TestHandleToolStart_GeneratesIDWhenEmpty(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetStreaming(model, true)

	var updated tea.Model = model

	// Empty toolID should generate one
	updated, _ = updated.Update(chat.ExportNewToolStartMsg("", "read_file", ""))
	modelState, isModel := updated.(*chat.Model)
	require.True(t, isModel)

	toolOrder := chat.ExportGetToolOrder(modelState)
	require.Len(t, toolOrder, 1)

	tools := chat.ExportGetTools(modelState)
	generatedID := toolOrder[0]
	assert.True(t, strings.HasPrefix(generatedID, "tool-"))
	assert.NotNil(t, tools[generatedID])
}

func TestHandleToolStart_MultipleTool(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetStreaming(model, true)

	var updated tea.Model = model

	updated, _ = updated.Update(chat.ExportNewToolStartMsg("t1", "bash", "ls"))
	updated, _ = updated.Update(chat.ExportNewToolStartMsg("t2", "read_file", "cat foo"))
	modelState, isModel := updated.(*chat.Model)
	require.True(t, isModel)

	assert.Equal(t, 2, chat.ExportGetPendingToolCount(modelState))
	assert.Len(t, chat.ExportGetToolOrder(modelState), 2)
}

// --- handleToolEnd tests ---

func TestHandleToolEnd_SuccessfulCompletion(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetStreaming(model, true)

	var updated tea.Model = model

	// Start a tool
	updated, _ = updated.Update(chat.ExportNewToolStartMsg("t1", "bash", "ls"))

	// Complete it successfully
	updated, _ = updated.Update(chat.ExportNewToolEndMsg("t1", "bash", "file1\nfile2", true))
	modelState, isModel := updated.(*chat.Model)
	require.True(t, isModel)

	tools := chat.ExportGetTools(modelState)
	require.Contains(t, tools, "t1")
	assert.Equal(t, chat.ToolStatusComplete, int(tools["t1"].Status()))
	assert.Equal(t, "file1\nfile2", tools["t1"].Output())
	assert.Equal(t, 0, chat.ExportGetPendingToolCount(modelState))
}

func TestHandleToolEnd_FailedCompletion(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetStreaming(model, true)

	var updated tea.Model = model

	updated, _ = updated.Update(chat.ExportNewToolStartMsg("t1", "bash", "rm /"))
	updated, _ = updated.Update(chat.ExportNewToolEndMsg("t1", "bash", "permission denied", false))
	modelState, isModel := updated.(*chat.Model)
	require.True(t, isModel)

	tools := chat.ExportGetTools(modelState)
	require.Contains(t, tools, "t1")
	assert.Equal(t, chat.ToolStatusFailed, int(tools["t1"].Status()))
}

func TestHandleToolEnd_MatchByName(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetStreaming(model, true)

	var updated tea.Model = model

	// Start tool with an ID
	updated, _ = updated.Update(chat.ExportNewToolStartMsg("t1", "bash", "ls"))

	// End with different ID but matching name
	updated, _ = updated.Update(chat.ExportNewToolEndMsg("different-id", "bash", "output", true))
	modelState, isModel := updated.(*chat.Model)
	require.True(t, isModel)

	tools := chat.ExportGetTools(modelState)
	require.Contains(t, tools, "t1")
	assert.Equal(t, chat.ToolStatusComplete, int(tools["t1"].Status()))
}

func TestHandleToolEnd_FIFOFallback(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetStreaming(model, true)

	var updated tea.Model = model

	// Start two tools
	updated, _ = updated.Update(chat.ExportNewToolStartMsg("t1", "bash", "ls"))
	updated, _ = updated.Update(chat.ExportNewToolStartMsg("t2", "read_file", "cat"))

	// End with unknown name and ID - should match first running tool (FIFO)
	updated, _ = updated.Update(chat.ExportNewToolEndMsg("", "unknown", "output", true))
	modelState, isModel := updated.(*chat.Model)
	require.True(t, isModel)

	tools := chat.ExportGetTools(modelState)
	assert.Equal(t, chat.ToolStatusComplete, int(tools["t1"].Status()))
	assert.Equal(t, chat.ToolStatusRunning, int(tools["t2"].Status()))
}

func TestHandleToolEnd_KeepsStreamedOutput(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetStreaming(model, true)

	var updated tea.Model = model

	// Start tool
	updated, _ = updated.Update(chat.ExportNewToolStartMsg("t1", "bash", "ls"))

	// Stream some output chunks
	updated, _ = updated.Update(chat.ExportNewToolOutputChunkMsg("bash", "streamed"))

	// End with SDK output - should NOT overwrite streamed output
	updated, _ = updated.Update(chat.ExportNewToolEndMsg("t1", "bash", "sdk-output", true))
	modelState, isModel := updated.(*chat.Model)
	require.True(t, isModel)

	tools := chat.ExportGetTools(modelState)
	assert.Equal(t, "streamed", tools["t1"].Output())
}

func TestHandleToolEnd_UsesSDKOutputWhenNoStreaming(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetStreaming(model, true)

	var updated tea.Model = model

	// Start and end tool without any output streaming
	updated, _ = updated.Update(chat.ExportNewToolStartMsg("t1", "bash", "ls"))
	updated, _ = updated.Update(chat.ExportNewToolEndMsg("t1", "bash", "sdk-output", true))
	modelState, isModel := updated.(*chat.Model)
	require.True(t, isModel)

	tools := chat.ExportGetTools(modelState)
	assert.Equal(t, "sdk-output", tools["t1"].Output())
}

func TestHandleToolEnd_PendingCountNeverNegative(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetStreaming(model, true)

	var updated tea.Model = model

	// Complete without starting - pendingToolCount should stay at 0
	updated, _ = updated.Update(chat.ExportNewToolEndMsg("t-nonexistent", "bash", "", true))
	modelState, isModel := updated.(*chat.Model)
	require.True(t, isModel)

	assert.GreaterOrEqual(t, chat.ExportGetPendingToolCount(modelState), 0)
}

// --- handleToolOutputChunk tests ---

func TestHandleToolOutputChunk_AppendsToRunningTool(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetStreaming(model, true)

	var updated tea.Model = model

	updated, _ = updated.Update(chat.ExportNewToolStartMsg("t1", "bash", "ls"))
	updated, _ = updated.Update(chat.ExportNewToolOutputChunkMsg("bash", "line1\n"))
	updated, _ = updated.Update(chat.ExportNewToolOutputChunkMsg("bash", "line2\n"))
	modelState, isModel := updated.(*chat.Model)
	require.True(t, isModel)

	tools := chat.ExportGetTools(modelState)
	assert.Equal(t, "line1\nline2\n", tools["t1"].Output())
}

func TestHandleToolOutputChunk_IgnoresUnknownTool(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetStreaming(model, true)

	var updated tea.Model = model

	// Should not panic when tool doesn't exist
	updated, _ = updated.Update(chat.ExportNewToolOutputChunkMsg("nonexistent", "output"))

	assert.NotNil(t, updated)
}

// --- handleStreamEnd tests ---

func TestHandleStreamEnd_FinalizesWhenNoTools(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetStreaming(model, true)

	// Add a streaming assistant message
	chat.ExportSetMessages(model, []chat.MessageForTest{
		chat.ExportNewStreamingAssistantMessage("response text"),
	})

	var updated tea.Model = model

	// Send some text first
	updated, _ = updated.Update(chat.ExportNewStreamChunkMsg("response text"))

	// Stream end should finalize since no tools are pending
	updated, _ = updated.Update(chat.ExportNewStreamEndMsg())
	modelState, isModel := updated.(*chat.Model)
	require.True(t, isModel)

	assert.False(t, chat.ExportGetStreaming(modelState))
	assert.True(t, chat.ExportGetJustCompleted(modelState))
	assert.False(t, chat.ExportGetMessageIsStreaming(modelState, 0))
}

func TestHandleStreamEnd_WaitsForPendingTools(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetStreaming(model, true)

	chat.ExportSetMessages(model, []chat.MessageForTest{
		chat.ExportNewStreamingAssistantMessage(""),
	})

	var updated tea.Model = model

	// Start a tool (increments pendingToolCount)
	updated, _ = updated.Update(chat.ExportNewToolStartMsg("t1", "bash", "ls"))

	// Stream end while tool is pending - should NOT finalize
	updated, _ = updated.Update(chat.ExportNewStreamEndMsg())
	modelState := requireModel(t, updated)

	// Should still be in streaming state because tool is pending
	assert.True(t, chat.ExportGetSessionComplete(modelState))
	assert.Equal(t, 1, chat.ExportGetPendingToolCount(modelState))
}

func TestHandleStreamEnd_FinalizesWhenToolsComplete(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetStreaming(model, true)

	chat.ExportSetMessages(model, []chat.MessageForTest{
		chat.ExportNewStreamingAssistantMessage(""),
	})

	var updated tea.Model = model

	// Start and complete a tool
	updated, _ = updated.Update(chat.ExportNewToolStartMsg("t1", "bash", "ls"))
	updated, _ = updated.Update(chat.ExportNewToolEndMsg("t1", "bash", "output", true))

	// Now stream end should finalize
	updated, _ = updated.Update(chat.ExportNewStreamEndMsg())
	m := requireModel(t, updated)

	assert.False(t, chat.ExportGetStreaming(m))
	assert.True(t, chat.ExportGetJustCompleted(m))
}

// --- handleTurnStart tests ---

func TestHandleTurnStart_SetsStreamingState(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetStreaming(model, false)
	chat.ExportSetJustCompleted(model, true)

	var updated tea.Model = model

	updated, _ = updated.Update(chat.ExportNewTurnStartMsg())
	m := requireModel(t, updated)

	assert.True(t, chat.ExportGetStreaming(m))
	assert.False(t, chat.ExportGetJustCompleted(m))
}

// --- handleTurnEnd tests ---

func TestHandleTurnEnd_ReturnsCommand(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetStreaming(model, true)

	var updated tea.Model = model

	// TurnEnd is informational; should return a cmd to keep waiting
	_, cmd := updated.Update(chat.ExportNewTurnEndMsg())

	assert.NotNil(t, cmd)
}

// --- handleReasoning tests ---

func TestHandleReasoning_SetsStreamingState(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetStreaming(model, false)
	chat.ExportSetJustCompleted(model, true)

	var updated tea.Model = model

	updated, _ = updated.Update(chat.ExportNewReasoningMsg("thinking...", true))
	m := requireModel(t, updated)

	assert.True(t, chat.ExportGetStreaming(m))
	assert.False(t, chat.ExportGetJustCompleted(m))
}

// --- handleAbort tests ---

func TestHandleAbort_StopsStreaming(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetStreaming(model, true)

	chat.ExportSetMessages(model, []chat.MessageForTest{
		chat.ExportNewStreamingAssistantMessage("partial response"),
	})

	var updated tea.Model = model

	updated, _ = updated.Update(chat.ExportNewAbortMsg())
	modelState := requireModel(t, updated)

	assert.False(t, chat.ExportGetStreaming(modelState))

	content := chat.ExportGetMessageContent(modelState, 0)
	assert.Contains(t, content, "[Session aborted]")
	assert.False(t, chat.ExportGetMessageIsStreaming(modelState, 0))
}

func TestHandleAbort_NoMessagesNoPanic(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetStreaming(model, true)
	chat.ExportSetMessages(model, nil)

	var updated tea.Model = model

	// Should not panic
	updated, _ = updated.Update(chat.ExportNewAbortMsg())
	modelState := requireModel(t, updated)

	assert.False(t, chat.ExportGetStreaming(modelState))
}

func TestHandleAbort_ReturnsNilCmd(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetStreaming(model, true)
	chat.ExportSetMessages(model, []chat.MessageForTest{
		chat.ExportNewStreamingAssistantMessage(""),
	})

	_, cmd := model.Update(chat.ExportNewAbortMsg())

	assert.Nil(t, cmd)
}

// --- handleStreamErr tests ---

func TestHandleStreamErr_SetsErrorState(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetStreaming(model, true)

	chat.ExportSetMessages(model, []chat.MessageForTest{
		chat.ExportNewStreamingAssistantMessage("partial"),
	})

	testErr := errStreamConnectionTimeout

	var updated tea.Model = model

	updated, _ = updated.Update(chat.ExportNewStreamErrMsg(testErr))
	modelState := requireModel(t, updated)

	assert.False(t, chat.ExportGetStreaming(modelState))
	assert.Equal(t, testErr, chat.ExportGetErr(modelState))

	content := chat.ExportGetMessageContent(modelState, 0)
	assert.Contains(t, content, "Error:")
	assert.False(t, chat.ExportGetMessageIsStreaming(modelState, 0))
}

func TestHandleStreamErr_ReturnsNilCmd(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetStreaming(model, true)
	chat.ExportSetMessages(model, []chat.MessageForTest{
		chat.ExportNewStreamingAssistantMessage(""),
	})

	_, cmd := model.Update(chat.ExportNewStreamErrMsg(errStreamTest))

	assert.Nil(t, cmd)
}

// --- handleSnapshotRewind tests ---

func TestHandleSnapshotRewind_AppendsIndicator(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetStreaming(model, true)

	chat.ExportSetMessages(model, []chat.MessageForTest{
		chat.ExportNewStreamingAssistantMessage("existing content"),
	})

	// Build up currentResponse to match message content
	var updated tea.Model = model

	updated, _ = updated.Update(chat.ExportNewStreamChunkMsg("existing content"))

	updated, _ = updated.Update(chat.ExportNewSnapshotRewindMsg())
	m := requireModel(t, updated)

	content := chat.ExportGetMessageContent(m, 0)
	assert.Contains(t, content, "[Session rewound to previous state]")
}

// --- handleUsage tests ---

func TestHandleUsage_UpdatesUsageState(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetStreaming(model, true)

	var updated tea.Model = model

	updated, _ = updated.Update(chat.ExportNewUsageMsg("gpt-4o", 100, 50, 0.005))
	m := requireModel(t, updated)

	assert.Equal(t, "gpt-4o", chat.ExportGetLastUsageModel(m))
}

func TestHandleUsage_MergesQuotaSnapshots(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetStreaming(model, true)

	// Set initial quota
	chat.ExportSetLastQuotaSnapshots(model, map[string]chat.QuotaSnapshotForTest{
		"chat": chat.ExportNewQuotaSnapshot(1000, 100, 90, false, "Jan 2"),
	})

	// Send usage with "premium" quota
	premiumQuota := map[string]chat.QuotaSnapshotForTest{
		"premium": chat.ExportNewQuotaSnapshot(500, 50, 90, false, "Jan 2"),
	}

	var updated tea.Model = model

	updated, _ = updated.Update(
		chat.ExportNewUsageMsgWithQuota("gpt-4o", 100, 50, 0.01, premiumQuota),
	)
	m := requireModel(t, updated)

	snapshots := chat.ExportGetLastQuotaSnapshots(m)
	assert.Contains(t, snapshots, "chat")
	assert.Contains(t, snapshots, "premium")
}

// --- handleCompaction tests ---

func TestHandleCompactionStart_SetsCompactingState(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetStreaming(model, true)

	var updated tea.Model = model

	updated, _ = updated.Update(chat.ExportNewCompactionStartMsg())
	m := requireModel(t, updated)

	assert.True(t, chat.ExportGetIsCompacting(m))
}

func TestHandleCompactionComplete_ClearsCompactingState(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetStreaming(model, true)

	var updated tea.Model = model

	updated, _ = updated.Update(chat.ExportNewCompactionStartMsg())
	updated, _ = updated.Update(chat.ExportNewCompactionCompleteMsg(true))
	m := requireModel(t, updated)

	assert.False(t, chat.ExportGetIsCompacting(m))
}

// --- handleIntent tests ---

func TestHandleIntent_AppendsToStreamingMessage(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetStreaming(model, true)

	chat.ExportSetMessages(model, []chat.MessageForTest{
		chat.ExportNewStreamingAssistantMessage(""),
	})

	var updated tea.Model = model

	updated, _ = updated.Update(chat.ExportNewIntentMsg("Planning approach"))
	m := requireModel(t, updated)

	content := chat.ExportGetMessageContent(m, 0)
	assert.Contains(t, content, "Planning approach")
}

func TestHandleIntent_IgnoresNonStreamingMessage(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetStreaming(model, true)

	chat.ExportSetMessages(model, []chat.MessageForTest{
		chat.ExportNewAssistantMessage("completed"),
	})

	var updated tea.Model = model

	updated, _ = updated.Update(chat.ExportNewIntentMsg("some intent"))
	m := requireModel(t, updated)

	// Content should not change since the message is not streaming
	assert.Equal(t, "completed", chat.ExportGetMessageContent(m, 0))
}

// --- handleModelChange tests ---

func TestHandleModelChange_UpdatesCurrentModel(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetStreaming(model, true)
	chat.ExportSetCurrentModel(model, "old-model")

	var updated tea.Model = model

	updated, _ = updated.Update(chat.ExportNewModelChangeMsg("old-model", "new-model"))
	m := requireModel(t, updated)

	assert.Equal(t, "new-model", chat.ExportGetCurrentModel(m))
}

func TestHandleModelChange_EmptyModelIgnored(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetStreaming(model, true)
	chat.ExportSetCurrentModel(model, "current")

	var updated tea.Model = model

	updated, _ = updated.Update(chat.ExportNewModelChangeMsg("current", ""))
	m := requireModel(t, updated)

	assert.Equal(t, "current", chat.ExportGetCurrentModel(m))
}

// --- handleShutdown tests ---

func TestHandleShutdown_StopsStreaming(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetStreaming(model, true)

	var updated tea.Model = model

	updated, _ = updated.Update(chat.ExportNewShutdownMsg())
	m := requireModel(t, updated)

	assert.False(t, chat.ExportGetStreaming(m))
}

func TestHandleShutdown_ReturnsNilCmd(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetStreaming(model, true)

	_, cmd := model.Update(chat.ExportNewShutdownMsg())

	assert.Nil(t, cmd)
}

// --- handleSystemNotification tests ---

func TestHandleSystemNotification_AppendsToStreamingMessage(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetStreaming(model, true)

	chat.ExportSetMessages(model, []chat.MessageForTest{
		chat.ExportNewStreamingAssistantMessage(""),
	})

	var updated tea.Model = model

	updated, _ = updated.Update(chat.ExportNewSystemNotificationMsg("System info"))
	m := requireModel(t, updated)

	content := chat.ExportGetMessageContent(m, 0)
	assert.Contains(t, content, "ℹ️ System info")
}

// --- handleSessionWarning tests ---

func TestHandleSessionWarning_AppendsToStreamingMessage(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetStreaming(model, true)

	chat.ExportSetMessages(model, []chat.MessageForTest{
		chat.ExportNewStreamingAssistantMessage(""),
	})

	var updated tea.Model = model

	updated, _ = updated.Update(chat.ExportNewSessionWarningMsg("Rate limited"))
	m := requireModel(t, updated)

	content := chat.ExportGetMessageContent(m, 0)
	assert.Contains(t, content, "⚠️ Rate limited")
}

// --- handleToolProgress tests ---

func TestHandleToolProgress_AppendsToTool(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetStreaming(model, true)

	var updated tea.Model = model

	updated, _ = updated.Update(chat.ExportNewToolStartMsg("t1", "bash", "ls"))

	// Send progress update
	updated, _ = updated.Update(chat.ToolProgressMsg{ToolID: "t1", Message: "50% complete"})
	modelState := requireModel(t, updated)

	tools := chat.ExportGetTools(modelState)
	require.Contains(t, tools, "t1")
	assert.Contains(t, tools["t1"].Output(), "⏳ 50% complete")
}

func TestHandleToolProgress_IgnoresNonRunningTool(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetStreaming(model, true)

	var updated tea.Model = model

	// Start and complete a tool
	updated, _ = updated.Update(chat.ExportNewToolStartMsg("t1", "bash", "ls"))
	updated, _ = updated.Update(chat.ExportNewToolEndMsg("t1", "bash", "done", true))

	// Progress update to completed tool should be ignored
	updated, _ = updated.Update(chat.ToolProgressMsg{ToolID: "t1", Message: "late update"})
	modelState := requireModel(t, updated)

	tools := chat.ExportGetTools(modelState)
	assert.NotContains(t, tools["t1"].Output(), "late update")
}

// --- handleTaskComplete tests ---

func TestHandleTaskComplete_AppendsToStreamingMessage(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetStreaming(model, true)

	chat.ExportSetMessages(model, []chat.MessageForTest{
		chat.ExportNewStreamingAssistantMessage(""),
	})

	var updated tea.Model = model

	updated, _ = updated.Update(chat.TaskCompleteMsg{Message: "Deploy succeeded"})
	m := requireModel(t, updated)

	content := chat.ExportGetMessageContent(m, 0)
	assert.Contains(t, content, "✅ Deploy succeeded")
}

// --- End-to-end stream scenario ---

func TestStreamFlow_FullConversation(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetStreaming(model, true)

	// Set up streaming assistant message
	chat.ExportSetMessages(model, []chat.MessageForTest{
		chat.ExportNewUserMessage("list clusters"),
		chat.ExportNewStreamingAssistantMessage(""),
	})

	var updated tea.Model = model

	// 1. Turn start
	updated, _ = updated.Update(chat.ExportNewTurnStartMsg())

	// 2. Stream text chunks
	updated, _ = updated.Update(chat.ExportNewStreamChunkMsg("I'll list "))
	updated, _ = updated.Update(chat.ExportNewStreamChunkMsg("your clusters."))

	// 3. Tool execution
	updated, _ = updated.Update(
		chat.ExportNewToolStartMsg("t1", "ksail_cluster_list", "ksail cluster list"),
	)
	updated, _ = updated.Update(
		chat.ExportNewToolOutputChunkMsg("ksail_cluster_list", "cluster-1\n"),
	)
	updated, _ = updated.Update(
		chat.ExportNewToolOutputChunkMsg("ksail_cluster_list", "cluster-2\n"),
	)
	updated, _ = updated.Update(chat.ExportNewToolEndMsg("t1", "ksail_cluster_list", "", true))

	// 4. More text
	updated, _ = updated.Update(chat.ExportNewStreamChunkMsg("\nYou have 2 clusters."))

	// 5. Final message
	updated, _ = updated.Update(chat.ExportNewAssistantMessageMsg(
		"I'll list your clusters.\nYou have 2 clusters."))

	// 6. Turn end + stream end
	updated, _ = updated.Update(chat.ExportNewTurnEndMsg())
	updated, _ = updated.Update(chat.ExportNewStreamEndMsg())
	modelState := requireModel(t, updated)

	// Verify final state
	assert.False(t, chat.ExportGetStreaming(modelState))
	assert.True(t, chat.ExportGetJustCompleted(modelState))
	assert.Equal(t, 0, chat.ExportGetPendingToolCount(modelState))

	// Check the assistant message content
	content := chat.ExportGetMessageContent(modelState, 1)
	assert.Contains(t, content, "clusters")

	// Check tool was tracked
	tools := chat.ExportGetTools(modelState)
	require.Contains(t, tools, "t1")
	assert.Equal(t, chat.ToolStatusComplete, int(tools["t1"].Status()))
}

// --- handleToolEnd with sessionComplete ---

func TestHandleToolEnd_FinalizesWhenSessionComplete(t *testing.T) {
	t.Parallel()

	model := chat.NewModel(newTestParams())
	chat.ExportSetStreaming(model, true)

	chat.ExportSetMessages(model, []chat.MessageForTest{
		chat.ExportNewStreamingAssistantMessage(""),
	})

	var updated tea.Model = model

	// Start a tool
	updated, _ = updated.Update(chat.ExportNewToolStartMsg("t1", "bash", "ls"))

	// StreamEnd arrives BEFORE tool completes (this is common)
	updated, _ = updated.Update(chat.ExportNewStreamEndMsg())

	// Tool completes AFTER streamEnd - should trigger finalization
	updated, _ = updated.Update(chat.ExportNewToolEndMsg("t1", "bash", "output", true))
	m := requireModel(t, updated)

	assert.False(t, chat.ExportGetStreaming(m))
	assert.True(t, chat.ExportGetJustCompleted(m))
}
