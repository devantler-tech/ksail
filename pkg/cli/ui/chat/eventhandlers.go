package chat

import (
	"errors"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	copilot "github.com/github/copilot-sdk/go"
)

// errStreamEvent is a sentinel error for stream event errors.
var errStreamEvent = errors.New("stream event error")

// sessionEventDispatcher routes SDK session events to the appropriate tea.Msg channel.
// It converts Copilot SDK events into chat-specific messages for the TUI.
type sessionEventDispatcher struct {
	eventChan       chan<- tea.Msg
	commandBuilders map[string]CommandBuilder
	toolNames       map[string]string // ToolCallID → toolName, for correlating start/complete
}

// newSessionEventDispatcher creates a dispatcher that routes events to the given channel.
func newSessionEventDispatcher(
	eventChan chan<- tea.Msg,
	commandBuilders map[string]CommandBuilder,
) *sessionEventDispatcher {
	return &sessionEventDispatcher{
		eventChan:       eventChan,
		commandBuilders: commandBuilders,
		toolNames:       make(map[string]string),
	}
}

// dispatch routes a Copilot session event to the appropriate handler.
//
//nolint:cyclop,funlen // type-switch dispatcher for session events
func (d *sessionEventDispatcher) dispatch(
	event copilot.SessionEvent,
) {
	//nolint:exhaustive // Only event types that affect dispatcher state are handled here.
	switch event.Type {
	case copilot.SessionEventTypeAssistantTurnStart:
		d.handleTurnStart()
	case copilot.SessionEventTypeAssistantMessageDelta:
		d.handleMessageDelta(event)
	case copilot.SessionEventTypeAssistantMessage:
		d.handleMessage(event)
	case copilot.SessionEventTypeAssistantReasoning,
		copilot.SessionEventTypeAssistantReasoningDelta:
		d.handleReasoning(event)
	case copilot.SessionEventTypeSessionIdle, copilot.SessionEventTypeAssistantTurnEnd:
		d.handleSessionLifecycle(event.Type)
	case copilot.SessionEventTypeAbort:
		d.handleAbort()
	case copilot.SessionEventTypeSessionError:
		d.handleSessionError(event)
	case copilot.SessionEventTypeToolExecutionStart:
		d.handleToolStart(event)
	case copilot.SessionEventTypeToolExecutionComplete:
		d.handleToolComplete(event)
	case copilot.SessionEventTypeSessionSnapshotRewind:
		d.handleSnapshotRewind()
	case copilot.SessionEventTypeAssistantUsage:
		d.handleUsage(event)
	case copilot.SessionEventTypeSessionCompactionStart:
		d.handleCompactionStart()
	case copilot.SessionEventTypeSessionCompactionComplete:
		d.handleCompactionComplete(event)
	case copilot.SessionEventTypeAssistantIntent:
		d.handleIntent(event)
	case copilot.SessionEventTypeSessionModelChange:
		d.handleModelChange(event)
	case copilot.SessionEventTypeSessionShutdown:
		d.handleShutdown(event)
	case copilot.SessionEventTypeSystemNotification:
		d.handleSystemNotification(event)
	case copilot.SessionEventTypeSessionWarning:
		d.handleSessionWarning(event)
	case copilot.SessionEventTypeToolExecutionProgress:
		d.handleToolProgress(event)
	case copilot.SessionEventTypeSessionTaskComplete:
		d.handleTaskComplete(event)
	}
}

func (d *sessionEventDispatcher) handleSessionLifecycle(eventType copilot.SessionEventType) {
	//nolint:exhaustive // Only lifecycle event types relevant to ending or transitioning a stream are handled here.
	switch eventType {
	case copilot.SessionEventTypeSessionIdle:
		d.eventChan <- streamEndMsg{}
	case copilot.SessionEventTypeAssistantTurnEnd:
		d.eventChan <- turnEndMsg{}
	default:
	}
}

func (d *sessionEventDispatcher) handleTurnStart() {
	d.eventChan <- turnStartMsg{}
}

func (d *sessionEventDispatcher) handleMessageDelta(event copilot.SessionEvent) {
	if data, ok := event.Data.(*copilot.AssistantMessageDeltaData); ok {
		d.eventChan <- streamChunkMsg{content: data.DeltaContent}
	}
}

func (d *sessionEventDispatcher) handleMessage(event copilot.SessionEvent) {
	if data, ok := event.Data.(*copilot.AssistantMessageData); ok {
		d.eventChan <- assistantMessageMsg{content: data.Content}
	}
}

func (d *sessionEventDispatcher) handleReasoning(event copilot.SessionEvent) {
	var content string

	switch data := event.Data.(type) {
	case *copilot.AssistantReasoningData:
		content = data.Content
	case *copilot.AssistantReasoningDeltaData:
		content = data.DeltaContent
	default:
		return
	}

	d.eventChan <- reasoningMsg{
		content: content,
		isDelta: event.Type == copilot.SessionEventTypeAssistantReasoningDelta,
	}
}

func (d *sessionEventDispatcher) handleAbort() {
	d.eventChan <- abortMsg{}
}

func (d *sessionEventDispatcher) handleSessionError(event copilot.SessionEvent) {
	errMsg := unknownErrorMsg

	if data, ok := event.Data.(*copilot.SessionErrorData); ok {
		errMsg = data.Message
	}

	d.eventChan <- streamErrMsg{err: fmt.Errorf("%w: %s", errStreamEvent, errMsg)}
}

func (d *sessionEventDispatcher) handleToolStart(event copilot.SessionEvent) {
	data, ok := event.Data.(*copilot.ToolExecutionStartData)
	if !ok {
		return
	}

	toolName := data.ToolName
	toolID := data.ToolCallID
	d.toolNames[toolID] = toolName

	var mcpServerName, mcpToolName string
	if data.McpServerName != nil {
		mcpServerName = *data.McpServerName
	}

	if data.McpToolName != nil {
		mcpToolName = *data.McpToolName
	}

	command := extractCommandFromArgs(toolName, data.Arguments, d.commandBuilders)

	d.eventChan <- toolStartMsg{
		toolID:        toolID,
		toolName:      toolName,
		command:       command,
		mcpServerName: mcpServerName,
		mcpToolName:   mcpToolName,
	}
}

func (d *sessionEventDispatcher) handleToolComplete(event copilot.SessionEvent) {
	data, ok := event.Data.(*copilot.ToolExecutionCompleteData)
	if !ok {
		return
	}

	toolID := data.ToolCallID
	toolName := d.toolNames[toolID]
	delete(d.toolNames, toolID)

	output := ""
	if data.Result != nil {
		output = data.Result.Content
	}

	d.eventChan <- toolEndMsg{toolID: toolID, toolName: toolName, output: output, success: data.Success}
}

func (d *sessionEventDispatcher) handleSnapshotRewind() {
	d.eventChan <- snapshotRewindMsg{}
}

func (d *sessionEventDispatcher) handleUsage(event copilot.SessionEvent) {
	data, ok := event.Data.(*copilot.AssistantUsageData)
	if !ok {
		return
	}

	msg := usageMsg{
		model: data.Model,
	}

	if data.InputTokens != nil {
		msg.inputTokens = *data.InputTokens
	}

	if data.OutputTokens != nil {
		msg.outputTokens = *data.OutputTokens
	}

	if data.Cost != nil {
		msg.cost = *data.Cost
	}

	if len(data.QuotaSnapshots) > 0 {
		msg.quotaSnapshots = make(map[string]quotaSnapshot, len(data.QuotaSnapshots))

		for key, snapshot := range data.QuotaSnapshots {
			resetStr := ""
			if snapshot.ResetDate != nil {
				resetStr = snapshot.ResetDate.Format("Jan 2")
			}

			msg.quotaSnapshots[key] = quotaSnapshot{
				entitlementRequests:   snapshot.EntitlementRequests,
				isUnlimited:           snapshot.IsUnlimitedEntitlement,
				usedRequests:          snapshot.UsedRequests,
				remainingPercentage:   snapshot.RemainingPercentage,
				resetDate:             resetStr,
				overage:               snapshot.Overage,
				overageAllowedAtQuota: snapshot.OverageAllowedWithExhaustedQuota,
			}
		}
	}

	d.eventChan <- msg
}

func (d *sessionEventDispatcher) handleCompactionStart() {
	d.eventChan <- compactionStartMsg{}
}

func (d *sessionEventDispatcher) handleCompactionComplete(event copilot.SessionEvent) {
	data, ok := event.Data.(*copilot.SessionCompactionCompleteData)
	if !ok {
		return
	}

	msg := compactionCompleteMsg{
		success: data.Success,
	}

	if data.PreCompactionTokens != nil {
		msg.preCompactionTokens = *data.PreCompactionTokens
	}

	if data.PostCompactionTokens != nil {
		msg.postCompactionTokens = *data.PostCompactionTokens
	}

	if data.TokensRemoved != nil {
		msg.tokensRemoved = *data.TokensRemoved
	}

	d.eventChan <- msg
}

func (d *sessionEventDispatcher) handleIntent(event copilot.SessionEvent) {
	if data, ok := event.Data.(*copilot.AssistantIntentData); ok {
		d.eventChan <- intentMsg{content: data.Intent}
	}
}

func (d *sessionEventDispatcher) handleModelChange(event copilot.SessionEvent) {
	data, ok := event.Data.(*copilot.SessionModelChangeData)
	if !ok {
		return
	}

	msg := modelChangeMsg{
		newModel: data.NewModel,
	}

	if data.PreviousModel != nil {
		msg.previousModel = *data.PreviousModel
	}

	d.eventChan <- msg
}

func (d *sessionEventDispatcher) handleShutdown(event copilot.SessionEvent) {
	data, ok := event.Data.(*copilot.SessionShutdownData)
	if !ok {
		return
	}

	d.eventChan <- shutdownMsg{
		shutdownType: string(data.ShutdownType),
	}
}

func (d *sessionEventDispatcher) handleSystemNotification(event copilot.SessionEvent) {
	data, ok := event.Data.(*copilot.SystemNotificationData)
	if !ok {
		return
	}

	d.eventChan <- systemNotificationMsg{
		message:  data.Content,
		infoType: string(data.Kind.Type),
	}
}

func (d *sessionEventDispatcher) handleSessionWarning(event copilot.SessionEvent) {
	data, ok := event.Data.(*copilot.SessionWarningData)
	if !ok {
		return
	}

	d.eventChan <- sessionWarningMsg{
		message:     data.Message,
		warningType: data.WarningType,
	}
}

func (d *sessionEventDispatcher) handleToolProgress(event copilot.SessionEvent) {
	data, ok := event.Data.(*copilot.ToolExecutionProgressData)
	if !ok {
		return
	}

	d.eventChan <- ToolProgressMsg{
		ToolID:  data.ToolCallID,
		Message: data.ProgressMessage,
	}
}

func (d *sessionEventDispatcher) handleTaskComplete(event copilot.SessionEvent) {
	data, ok := event.Data.(*copilot.SessionTaskCompleteData)
	if !ok {
		return
	}

	msg := ""
	if data.Summary != nil {
		msg = *data.Summary
	}

	d.eventChan <- TaskCompleteMsg{Message: msg}
}
