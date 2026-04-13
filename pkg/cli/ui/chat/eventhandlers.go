package chat

import (
	"errors"
	"fmt"
	"time"

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
}

// newSessionEventDispatcher creates a dispatcher that routes events to the given channel.
func newSessionEventDispatcher(
	eventChan chan<- tea.Msg,
	commandBuilders map[string]CommandBuilder,
) *sessionEventDispatcher {
	return &sessionEventDispatcher{
		eventChan:       eventChan,
		commandBuilders: commandBuilders,
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
	//nolint:exhaustive // Only message-producing event types are handled here.
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
	if event.Data.DeltaContent != nil {
		d.eventChan <- streamChunkMsg{content: *event.Data.DeltaContent}
	}
}

func (d *sessionEventDispatcher) handleMessage(event copilot.SessionEvent) {
	if event.Data.Content != nil {
		d.eventChan <- assistantMessageMsg{content: *event.Data.Content}
	}
}

func (d *sessionEventDispatcher) handleReasoning(event copilot.SessionEvent) {
	var content string
	if event.Data.Content != nil {
		content = *event.Data.Content
	} else if event.Data.DeltaContent != nil {
		content = *event.Data.DeltaContent
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
	if event.Data.Message != nil {
		errMsg = *event.Data.Message
	}

	d.eventChan <- streamErrMsg{err: fmt.Errorf("%w: %s", errStreamEvent, errMsg)}
}

func (d *sessionEventDispatcher) handleToolStart(event copilot.SessionEvent) {
	toolName := unknownToolName
	if event.Data.ToolName != nil {
		toolName = *event.Data.ToolName
	}

	var mcpServerName, mcpToolName string
	if event.Data.MCPServerName != nil {
		mcpServerName = *event.Data.MCPServerName
	}

	if event.Data.MCPToolName != nil {
		mcpToolName = *event.Data.MCPToolName
	}

	command := extractCommandFromArgs(toolName, event.Data.Arguments, d.commandBuilders)
	toolID := fmt.Sprintf(toolIDFormat, time.Now().UnixNano())

	d.eventChan <- toolStartMsg{
		toolID:        toolID,
		toolName:      toolName,
		command:       command,
		mcpServerName: mcpServerName,
		mcpToolName:   mcpToolName,
	}
}

func (d *sessionEventDispatcher) handleToolComplete(event copilot.SessionEvent) {
	toolName := unknownToolName
	if event.Data.ToolName != nil {
		toolName = *event.Data.ToolName
	}

	output := ""
	if event.Data.Result != nil && event.Data.Result.Content != nil {
		output = *event.Data.Result.Content
	}

	d.eventChan <- toolEndMsg{toolName: toolName, output: output, success: true}
}

func (d *sessionEventDispatcher) handleSnapshotRewind() {
	d.eventChan <- snapshotRewindMsg{}
}

func (d *sessionEventDispatcher) handleUsage(event copilot.SessionEvent) {
	msg := usageMsg{}

	if event.Data.Model != nil {
		msg.model = *event.Data.Model
	}

	if event.Data.InputTokens != nil {
		msg.inputTokens = *event.Data.InputTokens
	}

	if event.Data.OutputTokens != nil {
		msg.outputTokens = *event.Data.OutputTokens
	}

	if event.Data.Cost != nil {
		msg.cost = *event.Data.Cost
	}

	if len(event.Data.QuotaSnapshots) > 0 {
		msg.quotaSnapshots = make(map[string]quotaSnapshot, len(event.Data.QuotaSnapshots))

		for key, snapshot := range event.Data.QuotaSnapshots {
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
	msg := compactionCompleteMsg{}

	if event.Data.Success != nil {
		msg.success = *event.Data.Success
	}

	if event.Data.PreCompactionTokens != nil {
		msg.preCompactionTokens = *event.Data.PreCompactionTokens
	}

	if event.Data.PostCompactionTokens != nil {
		msg.postCompactionTokens = *event.Data.PostCompactionTokens
	}

	if event.Data.TokensRemoved != nil {
		msg.tokensRemoved = *event.Data.TokensRemoved
	}

	d.eventChan <- msg
}

func (d *sessionEventDispatcher) handleIntent(event copilot.SessionEvent) {
	if event.Data.Content != nil {
		d.eventChan <- intentMsg{content: *event.Data.Content}
	}
}

func (d *sessionEventDispatcher) handleModelChange(event copilot.SessionEvent) {
	msg := modelChangeMsg{}

	if event.Data.PreviousModel != nil {
		msg.previousModel = *event.Data.PreviousModel
	}

	if event.Data.NewModel != nil {
		msg.newModel = *event.Data.NewModel
	}

	d.eventChan <- msg
}

func (d *sessionEventDispatcher) handleShutdown(event copilot.SessionEvent) {
	msg := shutdownMsg{}

	if event.Data.ShutdownType != nil {
		msg.shutdownType = string(*event.Data.ShutdownType)
	}

	d.eventChan <- msg
}

func (d *sessionEventDispatcher) handleSystemNotification(event copilot.SessionEvent) {
	msg := systemNotificationMsg{}

	if event.Data.Message != nil {
		msg.message = *event.Data.Message
	}

	if event.Data.InfoType != nil {
		msg.infoType = *event.Data.InfoType
	}

	d.eventChan <- msg
}

func (d *sessionEventDispatcher) handleSessionWarning(event copilot.SessionEvent) {
	msg := sessionWarningMsg{}

	if event.Data.Message != nil {
		msg.message = *event.Data.Message
	}

	if event.Data.WarningType != nil {
		msg.warningType = *event.Data.WarningType
	}

	d.eventChan <- msg
}

func (d *sessionEventDispatcher) handleToolProgress(event copilot.SessionEvent) {
	if event.Data.ProgressMessage != nil && event.Data.ToolCallID != nil {
		d.eventChan <- ToolProgressMsg{
			ToolID:  *event.Data.ToolCallID,
			Message: *event.Data.ProgressMessage,
		}
	}
}

func (d *sessionEventDispatcher) handleTaskComplete(event copilot.SessionEvent) {
	msg := ""
	if event.Data.Summary != nil {
		msg = *event.Data.Summary
	}

	d.eventChan <- TaskCompleteMsg{Message: msg}
}
