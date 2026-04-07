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
