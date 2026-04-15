package chat

import (
	tea "github.com/charmbracelet/bubbletea"
	copilot "github.com/github/copilot-sdk/go"
)

// QuotaSnapshotForTest is an exported alias for quotaSnapshot, available only in test builds.
type QuotaSnapshotForTest = quotaSnapshot

// ExportSetStreaming sets Model.isStreaming for testing.
var ExportSetStreaming = func(m *Model, streaming bool) {
	m.isStreaming = streaming
}

// ExportSetAvailableModels sets Model.availableModels and filteredModels for testing.
var ExportSetAvailableModels = func(m *Model, models []copilot.ModelInfo) {
	m.availableModels = models
	m.filteredModels = models
}

// ExportSetCurrentModel sets Model.currentModel for testing.
var ExportSetCurrentModel = func(m *Model, model string) {
	m.currentModel = model
}

// ExportSetShowModelPicker sets Model.showModelPicker for testing.
var ExportSetShowModelPicker = func(m *Model, show bool) {
	m.showModelPicker = show
}

// ExportSetModelPickerIndex sets Model.modelPickerIndex for testing.
var ExportSetModelPickerIndex = func(m *Model, index int) {
	m.modelPickerIndex = index
}

// ExportSetShowReasoningPicker sets Model.showReasoningPicker for testing.
var ExportSetShowReasoningPicker = func(m *Model, show bool) {
	m.showReasoningPicker = show
}

// ExportSetReasoningPickerIndex sets Model.reasoningPickerIndex for testing.
var ExportSetReasoningPickerIndex = func(m *Model, index int) {
	m.reasoningPickerIndex = index
}

// ExportSetPendingPermission sets Model.pendingPermission for testing.
var ExportSetPendingPermission = func(m *Model, toolName, command, arguments string, response chan<- bool) {
	m.pendingPermission = &permissionRequestMsg{
		toolName:  toolName,
		command:   command,
		arguments: arguments,
		response:  response,
	}
}

// ExportGetPermissionHistoryLen returns the length of Model.permissionHistory for testing.
var ExportGetPermissionHistoryLen = func(m *Model) int {
	return len(m.permissionHistory)
}

// ExportGetPermissionHistoryLastAllowed returns whether the last permission was allowed.
var ExportGetPermissionHistoryLastAllowed = func(m *Model) bool {
	if len(m.permissionHistory) == 0 {
		return false
	}

	return m.permissionHistory[len(m.permissionHistory)-1].allowed
}

// ExportHasPendingPermission returns whether Model.pendingPermission is set.
var ExportHasPendingPermission = func(m *Model) bool {
	return m.pendingPermission != nil
}

// ExportSetLastQuotaSnapshots sets Model.lastQuotaSnapshots for testing.
var ExportSetLastQuotaSnapshots = func(m *Model, snapshots map[string]quotaSnapshot) {
	m.lastQuotaSnapshots = snapshots
}

// ExportNewQuotaSnapshot creates a quotaSnapshot for testing.
var ExportNewQuotaSnapshot = func(
	entitlement, used, remaining float64,
	isUnlimited bool,
	resetDate string,
) quotaSnapshot {
	return quotaSnapshot{
		entitlementRequests: entitlement,
		usedRequests:        used,
		remainingPercentage: remaining,
		isUnlimited:         isUnlimited,
		resetDate:           resetDate,
	}
}

// ExportBuildStatusText calls Model.buildStatusText for testing.
var ExportBuildStatusText = func(m *Model) string {
	return m.buildStatusText()
}

// ExportBuildQuotaStatusText calls Model.buildQuotaStatusText for testing.
var ExportBuildQuotaStatusText = func(m *Model) string {
	return m.buildQuotaStatusText()
}

// ExportSetSessionConfigModel sets the session config model for testing.
var ExportSetSessionConfigModel = func(m *Model, model string) {
	m.sessionConfig.Model = model
}

// ExportSetSessionConfigReasoningEffort sets the session config reasoning effort for testing.
var ExportSetSessionConfigReasoningEffort = func(m *Model, effort string) {
	m.sessionConfig.ReasoningEffort = effort
}

// ExportSetChatMode sets Model.chatMode for testing.
var ExportSetChatMode = func(m *Model, mode ChatMode) {
	m.chatMode = mode
}

// ExportGetChatMode returns Model.chatMode for testing.
var ExportGetChatMode = func(m *Model) ChatMode {
	return m.chatMode
}

// ExportSetLastUsageModel sets Model.lastUsageModel for testing.
var ExportSetLastUsageModel = func(m *Model, model string) {
	m.lastUsageModel = model
}

// ExportExtractPermissionDetails exposes extractPermissionDetails for testing.
var ExportExtractPermissionDetails = extractPermissionDetails

// ExportFormatPermissionKind exposes formatPermissionKind for testing.
var ExportFormatPermissionKind = formatPermissionKind

// ExportHumanizeToolName exposes humanizeToolName for testing.
var ExportHumanizeToolName = humanizeToolName

// ExportCalculatePickerScrollOffset exposes calculatePickerScrollOffset for testing.
var ExportCalculatePickerScrollOffset = calculatePickerScrollOffset

// ExportSetShowHelpOverlay sets Model.showHelpOverlay for testing.
var ExportSetShowHelpOverlay = func(m *Model, show bool) {
	m.showHelpOverlay = show
}

// ExportSetConfirmExit sets Model.confirmExit for testing.
var ExportSetConfirmExit = func(m *Model, confirm bool) {
	m.confirmExit = confirm
}

// ExportGetEventChannel exposes Model.GetEventChannel for testing.
var ExportGetEventChannel = func(m *Model) chan tea.Msg {
	return m.GetEventChannel()
}

// ExportExtractCommandFromArgs exposes extractCommandFromArgs for testing.
var ExportExtractCommandFromArgs = extractCommandFromArgs

// ExportValidateSessionID exposes validateSessionID for testing.
var ExportValidateSessionID = validateSessionID

// ExportIsValidSessionIDChar exposes isValidSessionIDChar for testing.
var ExportIsValidSessionIDChar = isValidSessionIDChar

// ExportDeleteLocalSession exposes deleteLocalSession for testing.
var ExportDeleteLocalSession = deleteLocalSession

// ErrSessionIDEmptyForTest exposes errSessionIDEmpty for testing.
var ErrSessionIDEmptyForTest = errSessionIDEmpty

// ErrInvalidSessionIDForTest exposes errInvalidSessionID for testing.
var ErrInvalidSessionIDForTest = errInvalidSessionID

// ErrInvalidAppDirForTest exposes errInvalidAppDir for testing.
var ErrInvalidAppDirForTest = errInvalidAppDir

// MessageForTest is an exported alias for the unexported message type, available only in test builds.
type MessageForTest = message

// ExportNewUserMessage creates a user message for testing GenerateSessionName.
var ExportNewUserMessage = func(content string) MessageForTest {
	return message{role: roleUser, content: content}
}

// ExportNewAssistantMessage creates an assistant message for testing GenerateSessionName.
var ExportNewAssistantMessage = func(content string) MessageForTest {
	return message{role: roleAssistant, content: content}
}

// ExportCurrentModelSupportsReasoning exposes currentModelSupportsReasoning for testing.
var ExportCurrentModelSupportsReasoning = func(m *Model) bool {
	return m.currentModelSupportsReasoning()
}

// ExportSetShowCopyFeedback sets Model.showCopyFeedback for testing.
var ExportSetShowCopyFeedback = func(m *Model, show bool) {
	m.showCopyFeedback = show
}

// ExportGetShowCopyFeedback returns Model.showCopyFeedback for testing.
var ExportGetShowCopyFeedback = func(m *Model) bool {
	return m.showCopyFeedback
}

// ExportNewCopyFeedbackClearMsg creates a copyFeedbackClearMsg for testing.
var ExportNewCopyFeedbackClearMsg = func() tea.Msg {
	return copyFeedbackClearMsg{}
}

// ExportNewModelUnavailableClearMsg creates a modelUnavailableClearMsg for testing.
var ExportNewModelUnavailableClearMsg = func() tea.Msg {
	return modelUnavailableClearMsg{}
}

// ExportSetHistory sets Model.history for testing.
var ExportSetHistory = func(m *Model, history []string) {
	m.history = history
	m.historyIndex = -1
}

// ExportGetHistoryIndex returns Model.historyIndex for testing.
var ExportGetHistoryIndex = func(m *Model) int {
	return m.historyIndex
}

// ExportSetMessages sets Model.messages for testing.
var ExportSetMessages = func(m *Model, msgs []MessageForTest) {
	m.messages = msgs
}

// ExportGetMessages returns Model.messages for testing.
var ExportGetMessages = func(m *Model) []MessageForTest {
	return m.messages
}

// ExportNewAssistantMessageWithRole creates an assistant message for testing.
var ExportNewAssistantMessageWithRole = func(content string) MessageForTest {
	return message{role: roleAssistant, content: content}
}

// ExportNewToolExecution creates a tool execution for testing.
var ExportNewToolExecution = func(name string, status int, expanded bool) *ToolExecutionForTest {
	return &toolExecution{name: name, status: toolStatus(status), expanded: expanded}
}

// ExportNewToolExecutionFull creates a tool execution with output and command for testing.
var ExportNewToolExecutionFull = func(
	name string,
	status int,
	expanded bool,
	command string,
	output string,
) *ToolExecutionForTest {
	return &toolExecution{
		name:     name,
		status:   toolStatus(status),
		expanded: expanded,
		command:  command,
		output:   output,
	}
}

// ExportNewToolExecutionWithPosition creates a tool execution with text position for testing.
var ExportNewToolExecutionWithPosition = func(
	name string,
	status int,
	expanded bool,
	command string,
	output string,
	textPosition int,
) *ToolExecutionForTest {
	return &toolExecution{
		name:         name,
		status:       toolStatus(status),
		expanded:     expanded,
		command:      command,
		output:       output,
		textPosition: textPosition,
	}
}

// ToolStatusFailed exposes the toolFailed constant for testing.
const ToolStatusFailed = int(toolFailed)

// ToolExecutionForTest is an exported alias for toolExecution, available only in test builds.
type ToolExecutionForTest = toolExecution

// ExportSetTools sets Model.tools and toolOrder for testing.
var ExportSetTools = func(m *Model, tools map[string]*ToolExecutionForTest, order []string) {
	m.tools = tools
	m.toolOrder = order
}

// ToolStatusRunning exposes the toolRunning constant for testing.
const ToolStatusRunning = int(toolRunning)

// ToolStatusComplete exposes the toolSuccess constant for testing.
const ToolStatusComplete = int(toolSuccess)

// ExportResolvedAutoModel exposes resolvedAutoModel for testing.
var ExportResolvedAutoModel = func(m *Model) string {
	return m.resolvedAutoModel()
}

// ExportFindModelMultiplier exposes findModelMultiplier for testing.
var ExportFindModelMultiplier = func(m *Model, modelID string) float64 {
	return m.findModelMultiplier(modelID)
}

// ExportIsAutoMode exposes isAutoMode for testing.
var ExportIsAutoMode = func(m *Model) bool {
	return m.isAutoMode()
}

// ExportFindCurrentModelIndex exposes findCurrentModelIndex for testing.
var ExportFindCurrentModelIndex = func(m *Model) int {
	return m.findCurrentModelIndex()
}

// ExportFindCurrentReasoningIndex exposes findCurrentReasoningIndex for testing.
var ExportFindCurrentReasoningIndex = func(m *Model) int {
	return m.findCurrentReasoningIndex()
}

// ExportIsCurrentReasoningEffort exposes isCurrentReasoningEffort for testing.
var ExportIsCurrentReasoningEffort = func(m *Model, level string) bool {
	return m.isCurrentReasoningEffort(level)
}

// ExportGetConfirmExit returns Model.confirmExit for testing.
var ExportGetConfirmExit = func(m *Model) bool {
	return m.confirmExit
}

// ExportGetQuitting returns Model.quitting for testing.
var ExportGetQuitting = func(m *Model) bool {
	return m.quitting
}

// ExportSetShowModelUnavailableFeedback sets Model.showModelUnavailableFeedback for testing.
var ExportSetShowModelUnavailableFeedback = func(m *Model, show bool) {
	m.showModelUnavailableFeedback = show
}

// ExportSetModelUnavailableReason sets Model.modelUnavailableReason for testing.
var ExportSetModelUnavailableReason = func(m *Model, reason string) {
	m.modelUnavailableReason = reason
}

// ExportFormatMultiplier exposes formatMultiplier for testing.
var ExportFormatMultiplier = formatMultiplier

// ExportBuildModelStatusText exposes buildModelStatusText for testing.
var ExportBuildModelStatusText = func(m *Model) string {
	return m.buildModelStatusText()
}

// ExportGetTextareaValue returns the current textarea value for testing.
var ExportGetTextareaValue = func(m *Model) string {
	return m.textarea.Value()
}

// ExportSetShowSessionPicker sets Model.showSessionPicker for testing.
var ExportSetShowSessionPicker = func(m *Model, show bool) {
	m.showSessionPicker = show
}

// ExportGetShowSessionPicker returns Model.showSessionPicker for testing.
var ExportGetShowSessionPicker = func(m *Model) bool {
	return m.showSessionPicker
}

// ExportTruncateString exposes truncateString for testing.
var ExportTruncateString = truncateString

// ExportAddToPromptHistory exposes addToPromptHistory for testing.
var ExportAddToPromptHistory = func(m *Model, prompt string) {
	m.addToPromptHistory(prompt)
}

// ExportGetHistory returns Model.history for testing.
var ExportGetHistory = func(m *Model) []string {
	return m.history
}

// ExportHasRunningTools exposes hasRunningTools for testing.
var ExportHasRunningTools = func(m *Model) bool {
	return m.hasRunningTools()
}

// ExportPeekNextPendingPrompt exposes peekNextPendingPrompt for testing.
var ExportPeekNextPendingPrompt = func(m *Model) bool {
	return m.peekNextPendingPrompt() != nil
}

// ExportDropNextPendingPrompt exposes dropNextPendingPrompt for testing.
var ExportDropNextPendingPrompt = func(m *Model) {
	m.dropNextPendingPrompt()
}

// ExportSetFilteredSessions sets Model.filteredSessions for testing.
var ExportSetFilteredSessions = func(m *Model, sessions []SessionMetadata) {
	m.filteredSessions = sessions
}

// ExportSetSessionPickerIndex sets Model.sessionPickerIndex for testing.
var ExportSetSessionPickerIndex = func(m *Model, index int) {
	m.sessionPickerIndex = index
}

// ExportIsValidSessionIndex exposes isValidSessionIndex for testing.
var ExportIsValidSessionIndex = func(m *Model) bool {
	return m.isValidSessionIndex()
}

// ExportIsInvalidSessionIndex exposes isInvalidSessionIndex for testing.
var ExportIsInvalidSessionIndex = func(m *Model) bool {
	return m.isInvalidSessionIndex()
}

// ExportClampSessionIndex exposes clampSessionIndex for testing.
var ExportClampSessionIndex = func(m *Model) {
	m.clampSessionIndex()
}

// ExportGetSessionPickerIndex returns Model.sessionPickerIndex for testing.
var ExportGetSessionPickerIndex = func(m *Model) int {
	return m.sessionPickerIndex
}

// ExportFindCurrentSessionIndex exposes findCurrentSessionIndex for testing.
var ExportFindCurrentSessionIndex = func(m *Model) int {
	return m.findCurrentSessionIndex()
}

// ExportSetCurrentSessionID sets Model.currentSessionID for testing.
var ExportSetCurrentSessionID = func(m *Model, id string) {
	m.currentSessionID = id
}

// ExportSetAvailableSessions sets Model.availableSessions for testing.
var ExportSetAvailableSessions = func(m *Model, sessions []SessionMetadata) {
	m.availableSessions = sessions
}

// ExportSetJustCompleted sets Model.justCompleted for testing.
var ExportSetJustCompleted = func(m *Model, completed bool) {
	m.justCompleted = completed
}

// ExportNewAssistantMessageWithTools creates an assistant message with tools for testing.
var ExportNewAssistantMessageWithTools = func(
	content string,
	tools []*toolExecution,
) MessageForTest {
	return message{
		role:    roleAssistant,
		content: content,
		tools:   tools,
	}
}

// ExportNewStreamingAssistantMessage creates a streaming assistant message for testing.
var ExportNewStreamingAssistantMessage = func(content string) MessageForTest {
	return message{
		role:        roleAssistant,
		content:     content,
		isStreaming: true,
	}
}

// ExportNewToolOutputMessage creates a legacy tool-output message for testing.
var ExportNewToolOutputMessage = func(content string) MessageForTest {
	return message{
		role:    "tool-output",
		content: content,
	}
}

// ExportCommitToolsToLastAssistantMessage exposes commitToolsToLastAssistantMessage for testing.
var ExportCommitToolsToLastAssistantMessage = func(m *Model) {
	m.commitToolsToLastAssistantMessage()
}

// ExportGetMessageToolCount returns the number of tools in a message for testing.
var ExportGetMessageToolCount = func(m *Model, msgIndex int) int {
	if msgIndex < 0 || msgIndex >= len(m.messages) {
		return -1
	}

	return len(m.messages[msgIndex].tools)
}

// --- Stream handler exports for black-box testing ---

// StreamChunkMsgForTest is an exported alias for streamChunkMsg.
type StreamChunkMsgForTest = streamChunkMsg

// AssistantMessageMsgForTest is an exported alias for assistantMessageMsg.
type AssistantMessageMsgForTest = assistantMessageMsg

// ToolStartMsgForTest is an exported alias for toolStartMsg.
type ToolStartMsgForTest = toolStartMsg

// ToolEndMsgForTest is an exported alias for toolEndMsg.
type ToolEndMsgForTest = toolEndMsg

// ToolOutputChunkMsgForTest is an exported alias for toolOutputChunkMsg.
type ToolOutputChunkMsgForTest = toolOutputChunkMsg

// StreamEndMsgForTest is an exported alias for streamEndMsg.
type StreamEndMsgForTest = streamEndMsg

// TurnStartMsgForTest is an exported alias for turnStartMsg.
type TurnStartMsgForTest = turnStartMsg

// TurnEndMsgForTest is an exported alias for turnEndMsg.
type TurnEndMsgForTest = turnEndMsg

// ReasoningMsgForTest is an exported alias for reasoningMsg.
type ReasoningMsgForTest = reasoningMsg

// AbortMsgForTest is an exported alias for abortMsg.
type AbortMsgForTest = abortMsg

// StreamErrMsgForTest is an exported alias for streamErrMsg.
type StreamErrMsgForTest = streamErrMsg

// SnapshotRewindMsgForTest is an exported alias for snapshotRewindMsg.
type SnapshotRewindMsgForTest = snapshotRewindMsg

// UsageMsgForTest is an exported alias for usageMsg.
type UsageMsgForTest = usageMsg

// CompactionStartMsgForTest is an exported alias for compactionStartMsg.
type CompactionStartMsgForTest = compactionStartMsg

// CompactionCompleteMsgForTest is an exported alias for compactionCompleteMsg.
type CompactionCompleteMsgForTest = compactionCompleteMsg

// IntentMsgForTest is an exported alias for intentMsg.
type IntentMsgForTest = intentMsg

// ModelChangeMsgForTest is an exported alias for modelChangeMsg.
type ModelChangeMsgForTest = modelChangeMsg

// ShutdownMsgForTest is an exported alias for shutdownMsg.
type ShutdownMsgForTest = shutdownMsg

// SystemNotificationMsgForTest is an exported alias for systemNotificationMsg.
type SystemNotificationMsgForTest = systemNotificationMsg

// SessionWarningMsgForTest is an exported alias for sessionWarningMsg.
type SessionWarningMsgForTest = sessionWarningMsg

// UserSubmitMsgForTest is an exported alias for userSubmitMsg.
type UserSubmitMsgForTest = userSubmitMsg

// ExportNewStreamChunkMsg creates a streamChunkMsg for testing.
//

var ExportNewStreamChunkMsg = func(content string) StreamChunkMsgForTest {
	return streamChunkMsg{content: content}
}

// ExportNewAssistantMessageMsg creates an assistantMessageMsg for testing.
//

var ExportNewAssistantMessageMsg = func(content string) AssistantMessageMsgForTest {
	return assistantMessageMsg{content: content}
}

// ExportNewToolStartMsg creates a toolStartMsg for testing.
//

var ExportNewToolStartMsg = func(toolID, toolName, command string) ToolStartMsgForTest {
	return toolStartMsg{toolID: toolID, toolName: toolName, command: command}
}

// ExportNewToolEndMsg creates a toolEndMsg for testing.
//

var ExportNewToolEndMsg = func(toolID, toolName, output string, success bool) ToolEndMsgForTest {
	return toolEndMsg{toolID: toolID, toolName: toolName, output: output, success: success}
}

// ExportNewToolOutputChunkMsg creates a toolOutputChunkMsg for testing.
//

var ExportNewToolOutputChunkMsg = func(toolID, chunk string) ToolOutputChunkMsgForTest {
	return toolOutputChunkMsg{toolID: toolID, chunk: chunk}
}

// ExportNewStreamEndMsg creates a streamEndMsg for testing.
//

var ExportNewStreamEndMsg = func() StreamEndMsgForTest {
	return streamEndMsg{}
}

// ExportNewTurnStartMsg creates a turnStartMsg for testing.
//

var ExportNewTurnStartMsg = func() TurnStartMsgForTest {
	return turnStartMsg{}
}

// ExportNewTurnEndMsg creates a turnEndMsg for testing.
//

var ExportNewTurnEndMsg = func() TurnEndMsgForTest {
	return turnEndMsg{}
}

// ExportNewReasoningMsg creates a reasoningMsg for testing.
//

var ExportNewReasoningMsg = func(content string, isDelta bool) ReasoningMsgForTest {
	return reasoningMsg{content: content, isDelta: isDelta}
}

// ExportNewAbortMsg creates an abortMsg for testing.
//

var ExportNewAbortMsg = func() AbortMsgForTest {
	return abortMsg{}
}

// ExportNewStreamErrMsg creates a streamErrMsg for testing.
//

var ExportNewStreamErrMsg = func(err error) StreamErrMsgForTest {
	return streamErrMsg{err: err}
}

// ExportNewSnapshotRewindMsg creates a snapshotRewindMsg for testing.
//

var ExportNewSnapshotRewindMsg = func() SnapshotRewindMsgForTest {
	return snapshotRewindMsg{}
}

// ExportNewUsageMsg creates a usageMsg for testing.
//

var ExportNewUsageMsg = func(model string, inputTokens, outputTokens, cost float64) UsageMsgForTest {
	return usageMsg{model: model, inputTokens: inputTokens, outputTokens: outputTokens, cost: cost}
}

// ExportNewUsageMsgWithQuota creates a usageMsg with quota snapshots for testing.
//

var ExportNewUsageMsgWithQuota = func(
	model string,
	inputTokens, outputTokens, cost float64,
	quotaSnapshots map[string]quotaSnapshot,
) UsageMsgForTest {
	return usageMsg{
		model:          model,
		inputTokens:    inputTokens,
		outputTokens:   outputTokens,
		cost:           cost,
		quotaSnapshots: quotaSnapshots,
	}
}

// ExportNewCompactionStartMsg creates a compactionStartMsg for testing.
//

var ExportNewCompactionStartMsg = func() CompactionStartMsgForTest {
	return compactionStartMsg{}
}

// ExportNewCompactionCompleteMsg creates a compactionCompleteMsg for testing.
//

var ExportNewCompactionCompleteMsg = func(success bool) CompactionCompleteMsgForTest {
	return compactionCompleteMsg{success: success}
}

// ExportNewIntentMsg creates an intentMsg for testing.
//

var ExportNewIntentMsg = func(content string) IntentMsgForTest {
	return intentMsg{content: content}
}

// ExportNewModelChangeMsg creates a modelChangeMsg for testing.
//

var ExportNewModelChangeMsg = func(previousModel, newModel string) ModelChangeMsgForTest {
	return modelChangeMsg{previousModel: previousModel, newModel: newModel}
}

// ExportNewShutdownMsg creates a shutdownMsg for testing.
//

var ExportNewShutdownMsg = func() ShutdownMsgForTest {
	return shutdownMsg{}
}

// ExportNewSystemNotificationMsg creates a systemNotificationMsg for testing.
//

var ExportNewSystemNotificationMsg = func(message string) SystemNotificationMsgForTest {
	return systemNotificationMsg{message: message}
}

// ExportNewSessionWarningMsg creates a sessionWarningMsg for testing.
//

var ExportNewSessionWarningMsg = func(message string) SessionWarningMsgForTest {
	return sessionWarningMsg{message: message}
}

// ExportGetStreaming returns Model.isStreaming for testing.
//

var ExportGetStreaming = func(m *Model) bool {
	return m.isStreaming
}

// ExportGetCurrentModel returns Model.currentModel for testing.
//

var ExportGetCurrentModel = func(m *Model) string {
	return m.currentModel
}

// ExportGetErr returns Model.err for testing.
//

var ExportGetErr = func(m *Model) error {
	return m.err
}

// ExportGetIsCompacting returns Model.isCompacting for testing.
//

var ExportGetIsCompacting = func(m *Model) bool {
	return m.isCompacting
}

// ExportGetLastUsageModel returns Model.lastUsageModel for testing.
//

var ExportGetLastUsageModel = func(m *Model) string {
	return m.lastUsageModel
}

// ExportGetLastQuotaSnapshots returns Model.lastQuotaSnapshots for testing.
//

var ExportGetLastQuotaSnapshots = func(m *Model) map[string]QuotaSnapshotForTest {
	return m.lastQuotaSnapshots
}

// ExportGetToolOrder returns Model.toolOrder for testing.
//

var ExportGetToolOrder = func(m *Model) []string {
	return m.toolOrder
}

// ExportGetTools returns Model.tools for testing.
//

var ExportGetTools = func(m *Model) map[string]*ToolExecutionForTest {
	return m.tools
}

// ExportGetPendingToolCount returns Model.pendingToolCount for testing.
//

var ExportGetPendingToolCount = func(m *Model) int {
	return m.pendingToolCount
}

// ExportGetSessionComplete returns Model.sessionComplete for testing.
//

var ExportGetSessionComplete = func(m *Model) bool {
	return m.sessionComplete
}

// ExportGetJustCompleted returns Model.justCompleted for testing.
//

var ExportGetJustCompleted = func(m *Model) bool {
	return m.justCompleted
}

// ExportPrepareForNewTurn exposes prepareForNewTurn for testing.
//

var ExportPrepareForNewTurn = func(m *Model) {
	m.prepareForNewTurn()
}

// ExportGetMessageContent returns the content of a message at the given index.
//

var ExportGetMessageContent = func(m *Model, idx int) string {
	if idx < 0 || idx >= len(m.messages) {
		return ""
	}

	return m.messages[idx].content
}

// ExportGetMessageRole returns the role of a message at the given index.
//

var ExportGetMessageRole = func(m *Model, idx int) string {
	if idx < 0 || idx >= len(m.messages) {
		return ""
	}

	return m.messages[idx].role
}

// ExportGetMessageIsStreaming returns whether a message is streaming at the given index.
//

var ExportGetMessageIsStreaming = func(m *Model, idx int) bool {
	if idx < 0 || idx >= len(m.messages) {
		return false
	}

	return m.messages[idx].isStreaming
}

// ExportSetUserScrolled sets Model.userScrolled for testing.
//

var ExportSetUserScrolled = func(m *Model, scrolled bool) {
	m.userScrolled = scrolled
}

// ExportGetUserScrolled returns Model.userScrolled for testing.
//

var ExportGetUserScrolled = func(m *Model) bool {
	return m.userScrolled
}

// ExportGetSavedInput returns Model.savedInput for testing.
//

var ExportGetSavedInput = func(m *Model) string {
	return m.savedInput
}

// Name returns the tool's name for testing.
func (t *toolExecution) Name() string { return t.name }

// Status returns the tool's status for testing.
func (t *toolExecution) Status() toolStatus { return t.status }

// Output returns the tool's output for testing.
func (t *toolExecution) Output() string { return t.output }

// Expanded returns whether the tool is expanded for testing.
func (t *toolExecution) Expanded() bool { return t.expanded }

// Command returns the tool's command for testing.
func (t *toolExecution) Command() string { return t.command }

// ID returns the tool's ID for testing.
func (t *toolExecution) ID() string { return t.id }

// --- Slash command test exports ---

// ExportClearViewportMsg is an exported alias for clearViewportMsg.
type ExportClearViewportMsg = clearViewportMsg

// ExportShowHelpMsg is an exported alias for showHelpMsg.
type ExportShowHelpMsg = showHelpMsg

// ExportModeChangeRequestMsg is an exported alias for modeChangeRequestMsg.
type ExportModeChangeRequestMsg = modeChangeRequestMsg

// ExportOpenModelPickerMsg is an exported alias for openModelPickerMsg.
type ExportOpenModelPickerMsg = openModelPickerMsg

// ExportModelSetRequestMsg is an exported alias for modelSetRequestMsg.
type ExportModelSetRequestMsg = modelSetRequestMsg

// ExportCommandDefinition wraps copilot.CommandDefinition for tests.
type ExportCommandDefinition struct {
	Def copilot.CommandDefinition
}

// ExportCommandContext is an exported alias for copilot.CommandContext.
type ExportCommandContext = copilot.CommandContext

// ExportCommandContextWithArgs creates a copilot.CommandContext with the given args.
func ExportCommandContextWithArgs(args string) copilot.CommandContext {
	return copilot.CommandContext{Args: args}
}

// --- Elicitation test exports ---

// ExportElicitationRequestMsg is an exported alias for elicitationRequestMsg.
type ExportElicitationRequestMsg = elicitationRequestMsg

// ExportElicitationResponsePayload is an exported alias for elicitationResponsePayload.
type ExportElicitationResponsePayload = elicitationResponsePayload

// ExportExtractFieldNames exposes extractFieldNames for testing.
var ExportExtractFieldNames = extractFieldNames

// --- Command picker test exports ---

// ExportUpdateCommandPicker exposes updateCommandPicker for testing.
var ExportUpdateCommandPicker = func(m *Model) {
	m.updateCommandPicker()
}

// ExportSetTextareaValue sets the textarea value for testing.
var ExportSetTextareaValue = func(m *Model, value string) {
	m.textarea.SetValue(value)
}

// ExportShowCommandPicker returns whether the command picker is visible.
var ExportShowCommandPicker = func(m *Model) bool {
	return m.showCommandPicker
}

// ExportFilteredCommands returns the filtered commands for testing.
var ExportFilteredCommands = func(m *Model) []copilot.CommandDefinition {
	return m.filteredCommands
}

// ExportCommandPickerIndex returns the current picker index for testing.
var ExportCommandPickerIndex = func(m *Model) int {
	return m.commandPickerIndex
}

// ExportSetCommandPickerIndex sets the picker index for testing.
var ExportSetCommandPickerIndex = func(m *Model, idx int) {
	m.commandPickerIndex = idx
}

// ExportGetSessionConfig returns the session config for testing.
var ExportGetSessionConfig = func(m *Model) *copilot.SessionConfig {
	return m.sessionConfig
}

// ExportCommandPickerExtraHeight exposes commandPickerExtraHeight for testing.
var ExportCommandPickerExtraHeight = func(m *Model) int {
	return m.commandPickerExtraHeight()
}

// --- Option picker test exports ---

// ExportCommandOption is an exported alias for CommandOption.
type ExportCommandOption = CommandOption

// ExportShowOptionPicker returns whether the option picker is visible.
var ExportShowOptionPicker = func(m *Model) bool {
	return m.showOptionPicker
}

// ExportFilteredOptions returns the filtered options for testing.
var ExportFilteredOptions = func(m *Model) []CommandOption {
	return m.filteredOptions
}

// ExportOptionPickerIndex returns the current option picker index.
var ExportOptionPickerIndex = func(m *Model) int {
	return m.optionPickerIndex
}

// ExportSetOptionPickerIndex sets the option picker index for testing.
var ExportSetOptionPickerIndex = func(m *Model, idx int) {
	m.optionPickerIndex = idx
}

// ExportActiveCommandName returns the active command name for the option picker.
var ExportActiveCommandName = func(m *Model) string {
	return m.activeCommandName
}

// ExportPickerExtraHeight exposes pickerExtraHeight for testing.
var ExportPickerExtraHeight = func(m *Model) int {
	return m.pickerExtraHeight()
}

// ExportSetCommandOptions sets the command options map for testing.
var ExportSetCommandOptions = func(m *Model, opts map[string]CommandOptionProvider) {
	m.commandOptions = opts
}

// --- Slash command dispatch test exports ---

// ExportTryDispatchSlashCommand exposes tryDispatchSlashCommand for testing.
var ExportTryDispatchSlashCommand = func(m *Model, content string) (bool, tea.Model, tea.Cmd) {
	return m.tryDispatchSlashCommand(content)
}
