package chat

import (
	tea "github.com/charmbracelet/bubbletea"
	copilot "github.com/github/copilot-sdk/go"
)

// QuotaSnapshotForTest is an exported alias for quotaSnapshot, available only in test builds.
type QuotaSnapshotForTest = quotaSnapshot //nolint:revive // exported test-only type alias

// ExportSetStreaming sets Model.isStreaming for testing.
//
//nolint:gochecknoglobals // export_test.go pattern requires global variables to expose internal functions
var ExportSetStreaming = func(m *Model, streaming bool) {
	m.isStreaming = streaming
}

// ExportSetAvailableModels sets Model.availableModels and filteredModels for testing.
//
//nolint:gochecknoglobals // export_test.go pattern requires global variables to expose internal functions
var ExportSetAvailableModels = func(m *Model, models []copilot.ModelInfo) {
	m.availableModels = models
	m.filteredModels = models
}

// ExportSetCurrentModel sets Model.currentModel for testing.
//
//nolint:gochecknoglobals // export_test.go pattern requires global variables to expose internal functions
var ExportSetCurrentModel = func(m *Model, model string) {
	m.currentModel = model
}

// ExportSetShowModelPicker sets Model.showModelPicker for testing.
//
//nolint:gochecknoglobals // export_test.go pattern requires global variables to expose internal functions
var ExportSetShowModelPicker = func(m *Model, show bool) {
	m.showModelPicker = show
}

// ExportSetModelPickerIndex sets Model.modelPickerIndex for testing.
//
//nolint:gochecknoglobals // export_test.go pattern requires global variables to expose internal functions
var ExportSetModelPickerIndex = func(m *Model, index int) {
	m.modelPickerIndex = index
}

// ExportSetShowReasoningPicker sets Model.showReasoningPicker for testing.
//
//nolint:gochecknoglobals // export_test.go pattern requires global variables to expose internal functions
var ExportSetShowReasoningPicker = func(m *Model, show bool) {
	m.showReasoningPicker = show
}

// ExportSetReasoningPickerIndex sets Model.reasoningPickerIndex for testing.
//
//nolint:gochecknoglobals // export_test.go pattern requires global variables to expose internal functions
var ExportSetReasoningPickerIndex = func(m *Model, index int) {
	m.reasoningPickerIndex = index
}

// ExportSetPendingPermission sets Model.pendingPermission for testing.
//
//nolint:gochecknoglobals // export_test.go pattern requires global variables to expose internal functions
var ExportSetPendingPermission = func(m *Model, toolName, command, arguments string, response chan<- bool) {
	m.pendingPermission = &permissionRequestMsg{
		toolName:  toolName,
		command:   command,
		arguments: arguments,
		response:  response,
	}
}

// ExportGetPermissionHistoryLen returns the length of Model.permissionHistory for testing.
//
//nolint:gochecknoglobals // export_test.go pattern requires global variables to expose internal functions
var ExportGetPermissionHistoryLen = func(m *Model) int {
	return len(m.permissionHistory)
}

// ExportGetPermissionHistoryLastAllowed returns whether the last permission was allowed.
//
//nolint:gochecknoglobals // export_test.go pattern requires global variables to expose internal functions
var ExportGetPermissionHistoryLastAllowed = func(m *Model) bool {
	if len(m.permissionHistory) == 0 {
		return false
	}

	return m.permissionHistory[len(m.permissionHistory)-1].allowed
}

// ExportHasPendingPermission returns whether Model.pendingPermission is set.
//
//nolint:gochecknoglobals // export_test.go pattern requires global variables to expose internal functions
var ExportHasPendingPermission = func(m *Model) bool {
	return m.pendingPermission != nil
}

// ExportSetLastQuotaSnapshots sets Model.lastQuotaSnapshots for testing.
//
//nolint:gochecknoglobals // export_test.go pattern requires global variables to expose internal functions
var ExportSetLastQuotaSnapshots = func(m *Model, snapshots map[string]quotaSnapshot) {
	m.lastQuotaSnapshots = snapshots
}

// ExportNewQuotaSnapshot creates a quotaSnapshot for testing.
//
//nolint:gochecknoglobals // export_test.go pattern requires global variables to expose internal functions
var ExportNewQuotaSnapshot = func(entitlement, used, remaining float64, isUnlimited bool, resetDate string) quotaSnapshot {
	return quotaSnapshot{
		entitlementRequests: entitlement,
		usedRequests:        used,
		remainingPercentage: remaining,
		isUnlimited:         isUnlimited,
		resetDate:           resetDate,
	}
}

// ExportBuildQuotaStatusText calls Model.buildQuotaStatusText for testing.
//
//nolint:gochecknoglobals // export_test.go pattern requires global variables to expose internal functions
var ExportBuildQuotaStatusText = func(m *Model) string {
	return m.buildQuotaStatusText()
}

// ExportSetSessionConfigModel sets the session config model for testing.
//
//nolint:gochecknoglobals // export_test.go pattern requires global variables to expose internal functions
var ExportSetSessionConfigModel = func(m *Model, model string) {
	m.sessionConfig.Model = model
}

// ExportSetSessionConfigReasoningEffort sets the session config reasoning effort for testing.
//
//nolint:gochecknoglobals // export_test.go pattern requires global variables to expose internal functions
var ExportSetSessionConfigReasoningEffort = func(m *Model, effort string) {
	m.sessionConfig.ReasoningEffort = effort
}

// ExportSetChatMode sets Model.chatMode for testing.
//
//nolint:gochecknoglobals // export_test.go pattern requires global variables to expose internal functions
var ExportSetChatMode = func(m *Model, mode ChatMode) {
	m.chatMode = mode
}

// ExportSetYoloMode sets Model.yoloMode for testing.
//
//nolint:gochecknoglobals // export_test.go pattern requires global variables to expose internal functions
var ExportSetYoloMode = func(m *Model, enabled bool) {
	m.yoloMode = enabled
}

// ExportSetLastUsageModel sets Model.lastUsageModel for testing.
//
//nolint:gochecknoglobals // export_test.go pattern requires global variables to expose internal functions
var ExportSetLastUsageModel = func(m *Model, model string) {
	m.lastUsageModel = model
}

// ExportExtractPermissionDetails exposes extractPermissionDetails for testing.
//
//nolint:gochecknoglobals // export_test.go pattern requires global variables to expose internal functions
var ExportExtractPermissionDetails = extractPermissionDetails

// ExportFormatPermissionKind exposes formatPermissionKind for testing.
//
//nolint:gochecknoglobals // export_test.go pattern requires global variables to expose internal functions
var ExportFormatPermissionKind = formatPermissionKind

// ExportExtractStringValue exposes extractStringValue for testing.
//
//nolint:gochecknoglobals // export_test.go pattern requires global variables to expose internal functions
var ExportExtractStringValue = extractStringValue

// ExportHumanizeToolName exposes humanizeToolName for testing.
//
//nolint:gochecknoglobals // export_test.go pattern requires global variables to expose internal functions
var ExportHumanizeToolName = humanizeToolName

// ExportCalculatePickerScrollOffset exposes calculatePickerScrollOffset for testing.
//
//nolint:gochecknoglobals // export_test.go pattern requires global variables to expose internal functions
var ExportCalculatePickerScrollOffset = calculatePickerScrollOffset

// ExportSetShowHelpOverlay sets Model.showHelpOverlay for testing.
//
//nolint:gochecknoglobals // export_test.go pattern requires global variables to expose internal functions
var ExportSetShowHelpOverlay = func(m *Model, show bool) {
	m.showHelpOverlay = show
}

// ExportSetConfirmExit sets Model.confirmExit for testing.
//
//nolint:gochecknoglobals // export_test.go pattern requires global variables to expose internal functions
var ExportSetConfirmExit = func(m *Model, confirm bool) {
	m.confirmExit = confirm
}

// ExportGetEventChannel exposes Model.GetEventChannel for testing.
//
//nolint:gochecknoglobals // export_test.go pattern requires global variables to expose internal functions
var ExportGetEventChannel = func(m *Model) chan tea.Msg {
	return m.GetEventChannel()
}

// ExportExtractCommandFromArgs exposes extractCommandFromArgs for testing.
//
//nolint:gochecknoglobals // export_test.go pattern requires global variables to expose internal functions
var ExportExtractCommandFromArgs = extractCommandFromArgs
