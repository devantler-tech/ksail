package chat

import (
	"errors"
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	copilot "github.com/github/copilot-sdk/go"
)

// sessionEventDispatcher routes SDK session events to the appropriate tea.Msg channel.
// It converts Copilot SDK events into chat-specific messages for the TUI.
type sessionEventDispatcher struct {
	eventChan chan<- tea.Msg
}

// newSessionEventDispatcher creates a dispatcher that routes events to the given channel.
func newSessionEventDispatcher(eventChan chan<- tea.Msg) *sessionEventDispatcher {
	return &sessionEventDispatcher{eventChan: eventChan}
}

// dispatch routes a Copilot session event to the appropriate handler.
func (d *sessionEventDispatcher) dispatch(event copilot.SessionEvent) {
	switch event.Type {
	case copilot.AssistantTurnStart:
		d.handleTurnStart()
	case copilot.AssistantMessageDelta:
		d.handleMessageDelta(event)
	case copilot.AssistantMessage:
		d.handleMessage(event)
	case copilot.AssistantReasoning, copilot.AssistantReasoningDelta:
		d.handleReasoning(event)
	case copilot.SessionIdle:
		d.handleSessionIdle()
	case copilot.AssistantTurnEnd:
		d.handleTurnEnd()
	case copilot.Abort:
		d.handleAbort()
	case copilot.SessionError:
		d.handleSessionError(event)
	case copilot.ToolExecutionStart:
		d.handleToolStart(event)
	case copilot.ToolExecutionComplete:
		d.handleToolComplete(event)
	case copilot.SessionSnapshotRewind:
		d.handleSnapshotRewind()
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
		isDelta: event.Type == copilot.AssistantReasoningDelta,
	}
}

func (d *sessionEventDispatcher) handleSessionIdle() {
	d.eventChan <- streamEndMsg{}
}

func (d *sessionEventDispatcher) handleTurnEnd() {
	d.eventChan <- turnEndMsg{}
}

func (d *sessionEventDispatcher) handleAbort() {
	d.eventChan <- abortMsg{}
}

func (d *sessionEventDispatcher) handleSessionError(event copilot.SessionEvent) {
	errMsg := unknownErrorMsg
	if event.Data.Message != nil {
		errMsg = *event.Data.Message
	}

	d.eventChan <- streamErrMsg{err: errors.New(errMsg)}
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

	command := extractCommandFromArgs(toolName, event.Data.Arguments)
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
	if event.Data.Result != nil {
		output = event.Data.Result.Content
	}

	d.eventChan <- toolEndMsg{toolName: toolName, output: output, success: true}
}

func (d *sessionEventDispatcher) handleSnapshotRewind() {
	d.eventChan <- snapshotRewindMsg{}
}
