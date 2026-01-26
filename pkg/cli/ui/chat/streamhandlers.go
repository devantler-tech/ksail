package chat

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// drainEventChannel drains any buffered events from the event channel.
func (m *Model) drainEventChannel() {
	for {
		select {
		case <-m.eventChan:
			// Discard stale event
		default:
			// Channel is empty
			return
		}
	}
}

// handleStreamChunk handles streaming response chunks.
func (m *Model) handleStreamChunk(msg streamChunkMsg) (tea.Model, tea.Cmd) {
	if msg.content != "" {
		m.currentResponse.WriteString(msg.content)
		if len(m.messages) > 0 {
			m.messages[len(m.messages)-1].content = m.currentResponse.String()
		}
		m.updateViewportContent()
	}
	return m, m.waitForEvent()
}

// handleAssistantMessage handles the final complete message from the assistant.
// Per SDK best practices, this event is always sent (regardless of streaming)
// and contains the complete response. It's more reliable for completion than
// accumulating deltas.
func (m *Model) handleAssistantMessage(msg assistantMessageMsg) (tea.Model, tea.Cmd) {
	// If we have more complete content from the final message, use it
	// This handles cases where deltas might have been missed
	if len(msg.content) > m.currentResponse.Len() {
		m.currentResponse.Reset()
		m.currentResponse.WriteString(msg.content)
	}

	// Update the message content
	if len(m.messages) > 0 {
		last := &m.messages[len(m.messages)-1]
		if last.role == "assistant" {
			last.content = m.currentResponse.String()
			// Don't render yet - wait for SessionIdle or TurnEnd for proper completion
		}
	}

	m.updateViewportContent()
	return m, m.waitForEvent()
}

// handleToolStart handles tool execution start events.
func (m *Model) handleToolStart(msg toolStartMsg) (tea.Model, tea.Cmd) {
	// Generate tool ID if not provided
	toolID := msg.toolID
	if toolID == "" {
		toolID = fmt.Sprintf("tool-%d", time.Now().UnixNano())
	}

	// Record current position in assistant response for interleaving
	textPos := m.currentResponse.Len()

	// Create tool execution entry
	tool := &toolExecution{
		id:           toolID,
		name:         msg.toolName,
		command:      msg.command,
		status:       toolRunning,
		expanded:     true, // expanded by default while running
		startTime:    time.Now(),
		textPosition: textPos,
	}
	m.tools[toolID] = tool
	m.toolOrder = append(m.toolOrder, toolID)

	// Track pending tools for proper completion detection
	m.pendingToolCount++
	m.lastToolCall = time.Now()

	// DON'T insert tool as separate message - render inline with assistant response
	m.updateViewportContent()
	return m, m.waitForEvent()
}

// handleToolEnd handles tool execution completion events.
func (m *Model) handleToolEnd(msg toolEndMsg) (tea.Model, tea.Cmd) {
	// Find the tool to complete. The SDK's ToolExecutionComplete event often
	// doesn't include the tool name, so we use FIFO matching as primary strategy.
	var tool *toolExecution

	// Strategy 1: Try matching by tool ID if provided
	if msg.toolID != "" {
		tool = m.tools[msg.toolID]
	}

	// Strategy 2: Try matching by name if provided and not "unknown"
	if tool == nil && msg.toolName != "" && msg.toolName != "unknown" {
		for _, id := range m.toolOrder {
			t := m.tools[id]
			if t != nil && t.name == msg.toolName && t.status == toolRunning {
				tool = t
				break
			}
		}
	}

	// Strategy 3: FIFO - match the first running tool (SDK doesn't always provide name)
	if tool == nil {
		for _, id := range m.toolOrder {
			t := m.tools[id]
			if t != nil && t.status == toolRunning {
				tool = t
				break
			}
		}
	}

	if tool != nil {
		// Update tool status
		if msg.success {
			tool.status = toolSuccess
		} else {
			tool.status = toolFailed
		}
		// Only use SDK output if we didn't stream any output already
		if tool.output == "" && msg.output != "" {
			tool.output = msg.output
		}
		// Keep expanded so users can follow along with output (press Tab to collapse)
		tool.expanded = true

		// Track pending tools for proper completion detection
		m.pendingToolCount--
		if m.pendingToolCount < 0 {
			m.pendingToolCount = 0 // Safety guard
		}
	}

	m.updateViewportContent()

	// Check if we can finalize (sessionComplete and no pending tools)
	if m.sessionComplete && m.pendingToolCount == 0 {
		return m.tryFinalizeResponse()
	}

	// Always keep waiting for events after tool completion.
	// The SDK will fire another turn for the assistant to process results and respond.
	// SessionIdle will signal when the entire turn (including tool result processing) is complete.
	return m, m.waitForEvent()
}

// handleToolOutputChunk handles real-time output chunks from running tools.
func (m *Model) handleToolOutputChunk(toolID, chunk string) (tea.Model, tea.Cmd) {
	// The toolID from generator is actually the tool name (e.g., "ksail_cluster_list")
	// Find the FIRST running tool that matches this name (FIFO order)
	var tool *toolExecution
	for _, id := range m.toolOrder {
		t := m.tools[id]
		if t != nil && t.name == toolID && t.status == toolRunning {
			tool = t
			break
		}
	}

	if tool != nil {
		// Append the chunk to the tool's output
		tool.output += chunk
		m.updateViewportContent()
	}

	// Always keep waiting - more output chunks or events (like turn completion) may come
	return m, m.waitForEvent()
}

// handleStreamEnd handles stream completion events (SessionIdle).
// SessionIdle means the session has truly finished processing.
// Per SDK best practices, SessionIdle is the authoritative signal for turn completion.
func (m *Model) handleStreamEnd() (tea.Model, tea.Cmd) {
	// Mark session as complete
	m.sessionComplete = true

	// Check if we can finalize (no pending tools)
	if m.pendingToolCount == 0 {
		return m.tryFinalizeResponse()
	}

	// Still have pending tools - wait for them to complete
	// The tool end handlers will check sessionComplete and finalize when ready
	return m, m.waitForEvent()
}

// tryFinalizeResponse attempts to finalize the assistant response.
// Called when both sessionComplete is true AND pendingToolCount is 0.
// This ensures all tool events have been processed before we stop listening.
func (m *Model) tryFinalizeResponse() (tea.Model, tea.Cmd) {
	// Finalize the response
	m.isStreaming = false
	m.justCompleted = true // Show "Ready" indicator

	if len(m.messages) > 0 {
		last := &m.messages[len(m.messages)-1]
		last.isStreaming = false
		last.content = m.currentResponse.String()
		// Render markdown using cached renderer (avoids terminal queries)
		last.rendered = renderMarkdownWithRenderer(m.renderer, last.content)

		// Commit current tools to this message for persistence across turns
		last.tools = make([]*toolExecution, 0, len(m.toolOrder))
		last.toolOrder = make([]string, len(m.toolOrder))
		copy(last.toolOrder, m.toolOrder)
		for _, id := range m.toolOrder {
			if tool := m.tools[id]; tool != nil {
				last.tools = append(last.tools, tool)
			}
		}
	}

	m.updateViewportContent()
	m.cleanup() // Clean up event subscription

	// Auto-save session after each completed turn
	_ = m.saveCurrentSession()

	return m, nil
}

// handleTurnEnd handles assistant turn end events (AssistantTurnEnd).
// AssistantTurnEnd fires after each turn, including intermediate turns where the
// assistant calls tools. We always wait for SessionIdle for authoritative completion.
func (m *Model) handleTurnEnd() (tea.Model, tea.Cmd) {
	// TurnEnd is informational - always wait for SessionIdle for completion
	// This ensures tool results are fully processed before we stop listening
	return m, m.waitForEvent()
}

// handleTurnStart handles assistant turn start events (AssistantTurnStart).
// This fires when the assistant begins a new turn, ensuring we're in streaming mode.
func (m *Model) handleTurnStart() (tea.Model, tea.Cmd) {
	// Ensure we're in streaming mode when a new turn starts
	m.isStreaming = true
	m.justCompleted = false
	m.updateViewportContent()
	return m, m.waitForEvent()
}

// handleReasoning handles reasoning events from the assistant.
// These indicate the LLM is actively "thinking" about the response.
func (m *Model) handleReasoning(_ reasoningMsg) (tea.Model, tea.Cmd) {
	// Reasoning events confirm the LLM is actively working
	// We just keep streaming state active and wait for more events
	m.isStreaming = true
	m.justCompleted = false
	// Optionally, we could append reasoning content to a separate buffer
	// For now, just acknowledge we're still processing
	m.updateViewportContent()
	return m, m.waitForEvent()
}

// handleAbort handles session abort events.
func (m *Model) handleAbort() (tea.Model, tea.Cmd) {
	m.isStreaming = false
	m.cleanup()
	if len(m.messages) > 0 && m.messages[len(m.messages)-1].role == "assistant" {
		last := &m.messages[len(m.messages)-1]
		last.content += "\n\n[Session aborted]"
		last.isStreaming = false
		last.rendered = renderMarkdownWithRenderer(m.renderer, last.content)
	}
	m.updateViewportContent()
	return m, nil
}

// handleStreamErr handles streaming error events.
func (m *Model) handleStreamErr(msg streamErrMsg) (tea.Model, tea.Cmd) {
	m.isStreaming = false
	m.cleanup() // Clean up event subscription
	m.err = msg.err
	if len(m.messages) > 0 && m.messages[len(m.messages)-1].role == "assistant" {
		last := &m.messages[len(m.messages)-1]
		last.content = fmt.Sprintf("Error: %v", msg.err)
		last.isStreaming = false
		last.rendered = renderMarkdownWithRenderer(m.renderer, last.content)
	}
	m.updateViewportContent()
	// Don't wait for more events - response is complete (with error)
	return m, nil
}

// waitForEvent returns a command that waits for an event from the channel.
// Uses select to allow context cancellation.
func (m *Model) waitForEvent() tea.Cmd {
	ctx := m.ctx
	eventChan := m.eventChan
	timeout := m.timeout
	return func() tea.Msg {
		// Use timeout to detect stuck conditions (e.g., assistant looping on tools)
		timer := time.NewTimer(timeout)
		defer timer.Stop()

		select {
		case msg := <-eventChan:
			return msg
		case <-timer.C:
			return streamErrMsg{
				err: fmt.Errorf(
					"response timed out after %v - the assistant may be stuck",
					timeout,
				),
			}
		case <-ctx.Done():
			return streamErrMsg{err: ctx.Err()}
		}
	}
}

// cleanup releases resources when streaming ends or is cancelled.
func (m *Model) cleanup() {
	// Unsubscribe from session events (thread-safe)
	m.unsubscribeMu.Lock()
	if m.unsubscribe != nil {
		m.unsubscribe()
		m.unsubscribe = nil
	}
	m.unsubscribeMu.Unlock()

	// Reset completion tracking state
	m.sessionComplete = false
	m.pendingToolCount = 0

	// Drain any remaining events to prevent blocking
	m.drainEventChannel()
}
