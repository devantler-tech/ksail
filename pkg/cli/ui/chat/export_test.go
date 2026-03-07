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

// ExportSetYoloMode sets Model.yoloMode for testing.
var ExportSetYoloMode = func(m *Model, enabled bool) {
	m.yoloMode = enabled
}

// ExportSetLastUsageModel sets Model.lastUsageModel for testing.
var ExportSetLastUsageModel = func(m *Model, model string) {
	m.lastUsageModel = model
}

// ExportExtractPermissionDetails exposes extractPermissionDetails for testing.
var ExportExtractPermissionDetails = extractPermissionDetails

// ExportFormatPermissionKind exposes formatPermissionKind for testing.
var ExportFormatPermissionKind = formatPermissionKind

// ExportExtractStringValue exposes extractStringValue for testing.
var ExportExtractStringValue = extractStringValue

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

// ExportDeleteOrphanedLocalSessions exposes deleteOrphanedLocalSessions for testing.
var ExportDeleteOrphanedLocalSessions = deleteOrphanedLocalSessions

// ErrSessionIDEmptyForTest exposes errSessionIDEmpty for testing.
var ErrSessionIDEmptyForTest = errSessionIDEmpty

// ErrInvalidSessionIDForTest exposes errInvalidSessionID for testing.
var ErrInvalidSessionIDForTest = errInvalidSessionID

// ErrInvalidAppDirForTest exposes errInvalidAppDir for testing.
var ErrInvalidAppDirForTest = errInvalidAppDir

// MessageForTest is an exported alias for the unexported message type, available only in test builds.
type MessageForTest = message

// ExportNewUserMessage creates a user message for testing GenerateSessionName.
var ExportNewUserMessage = func(content string) message {
	return message{role: roleUser, content: content}
}

// ExportNewAssistantMessage creates an assistant message for testing GenerateSessionName.
var ExportNewAssistantMessage = func(content string) message {
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
