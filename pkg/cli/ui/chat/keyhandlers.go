package chat

import (
	"strings"
	"time"

	"github.com/atotto/clipboard"
	tea "github.com/charmbracelet/bubbletea"
)

const (
	// clipboardResetMillis is the delay before clearing copy feedback.
	clipboardResetMillis = 1500
)

// handleKeyMsg handles keyboard input.
func (m *Model) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Handle help overlay with highest priority (only F1 or esc can close it)
	if m.showHelpOverlay {
		return m.handleHelpOverlayKey(msg)
	}

	// Handle overlays first (highest priority)
	if m.pendingPermission != nil {
		return m.handlePermissionKey(msg)
	}
	if m.showModelPicker {
		return m.handleModelPickerKey(msg)
	}
	if m.showSessionPicker {
		return m.handleSessionPickerKey(msg)
	}

	// Handle main key events and viewport scrolling
	return m.handleChatKey(msg)
}

// handleHelpOverlayKey handles keyboard input when the help overlay is visible.
func (m *Model) handleHelpOverlayKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "f1", keyEscape:
		m.showHelpOverlay = false
		return m, nil
	}

	return m, nil
}

// handleChatKey handles keyboard input in the main chat view.
func (m *Model) handleChatKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case keyCtrlC:
		return m.handleQuit(true)
	case keyEscape:
		return m.handleEscape()
	case "f1":
		m.showHelpOverlay = true
		return m, nil
	case "ctrl+o":
		return m.handleOpenModelPicker()
	case "ctrl+h":
		return m.handleOpenSessionPicker()
	case "ctrl+n":
		return m.handleNewChat()
	case "alt+enter":
		m.textarea.InsertString("\n")
		return m, nil
	case "tab":
		return m.handleToggleMode()
	case "ctrl+t":
		return m.handleToggleAllTools()
	case "ctrl+y":
		return m.handleCopyOutput()
	case "up":
		return m.handleHistoryUp()
	case keyDown:
		return m.handleHistoryDown()
	case keyEnter:
		return m.handleEnter()
	}

	return m.handleViewportAndTextareaKey(msg)
}

// handleViewportAndTextareaKey handles page up/down and textarea input.
func (m *Model) handleViewportAndTextareaKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Handle page up/down for viewport scrolling
	//nolint:exhaustive // Only page up/down are relevant for viewport scrolling.
	switch msg.Type {
	case tea.KeyPgUp:
		m.viewport.HalfPageUp()
		m.userScrolled = !m.viewport.AtBottom()

		return m, nil
	case tea.KeyPgDown:
		m.viewport.HalfPageDown()

		if m.viewport.AtBottom() {
			m.userScrolled = false
		}

		return m, nil
	}

	// Update textarea for other keys
	var taCmd tea.Cmd
	m.textarea, taCmd = m.textarea.Update(msg)
	if m.justCompleted {
		m.justCompleted = false
	}

	return m, taCmd
}

// handleQuit handles application quit.
func (m *Model) handleQuit(saveSession bool) (tea.Model, tea.Cmd) {
	m.cleanup()
	if saveSession {
		_ = m.saveCurrentSession()
	}
	m.quitting = true
	return m, tea.Quit
}

// handleEscape handles the escape key.
func (m *Model) handleEscape() (tea.Model, tea.Cmd) {
	if m.isStreaming {
		m.cleanup()

		m.isStreaming = false

		if len(m.messages) > 0 && m.messages[len(m.messages)-1].role == roleAssistant {
			last := &m.messages[len(m.messages)-1]
			last.content += " [cancelled]"
			last.isStreaming = false
			last.rendered = renderMarkdownWithRenderer(m.renderer, last.content)
		}

		m.updateViewportContent()

		return m, nil
	}

	return m.handleQuit(true)
}

// handleOpenModelPicker opens the model selection picker.
func (m *Model) handleOpenModelPicker() (tea.Model, tea.Cmd) {
	if m.isStreaming || len(m.availableModels) == 0 {
		return m, nil
	}
	m.showModelPicker = true
	m.filteredModels = m.availableModels // Start with all models
	m.modelFilterText = ""               // Reset filter
	m.modelFilterActive = false          // Start in navigation mode
	m.updateDimensions()
	m.modelPickerIndex = m.findCurrentModelIndex()
	return m, nil
}

// findCurrentModelIndex returns the picker index for the current model.
func (m *Model) findCurrentModelIndex() int {
	if m.currentModel == "" || m.currentModel == modelAuto {
		return 0
	}

	for idx, model := range m.filteredModels {
		if model.ID == m.currentModel {
			return idx + 1 // offset by 1 for auto option
		}
	}
	return 0
}

// handleOpenSessionPicker opens the session history picker.
func (m *Model) handleOpenSessionPicker() (tea.Model, tea.Cmd) {
	if m.isStreaming {
		return m, nil
	}
	sessions, _ := ListSessions(m.client)
	m.availableSessions = sessions
	m.filteredSessions = sessions // Start with all sessions
	m.sessionFilterText = ""      // Reset filter
	m.sessionFilterActive = false // Start in navigation mode
	m.showSessionPicker = true
	m.confirmDeleteSession = false
	m.updateDimensions()
	m.sessionPickerIndex = m.findCurrentSessionIndex()
	return m, nil
}

// findCurrentSessionIndex returns the picker index for the current session.
func (m *Model) findCurrentSessionIndex() int {
	if m.currentSessionID == "" {
		return 0
	}

	for idx, session := range m.availableSessions {
		if session.ID == m.currentSessionID {
			return idx + 1 // offset by 1 for "New Chat" option
		}
	}
	return 0
}

// handleNewChat creates a new chat session.
func (m *Model) handleNewChat() (tea.Model, tea.Cmd) {
	if m.isStreaming {
		return m, nil
	}
	_ = m.saveCurrentSession()

	err := m.startNewSession()
	if err != nil {
		m.err = err
	}
	return m, nil
}

// handleToggleMode toggles between agent and plan mode.
func (m *Model) handleToggleMode() (tea.Model, tea.Cmd) {
	// Prevent mode toggling while streaming to avoid mode mismatch between
	// message submission and tool execution time
	if m.isStreaming {
		return m, nil
	}
	m.agentMode = !m.agentMode
	// Update the shared reference so tool handlers see the change
	if m.agentModeRef != nil {
		m.agentModeRef.SetEnabled(m.agentMode)
	}
	m.updateViewportContent()
	return m, nil
}

// handleToggleAllTools toggles tool expansion for all messages.
func (m *Model) handleToggleAllTools() (tea.Model, tea.Cmd) {
	expandAll := m.findFirstToolExpandState()
	m.setAllToolsExpanded(expandAll)
	m.updateViewportContent()
	return m, nil
}

// findFirstToolExpandState finds the expand state of the first tool.
// Returns the inverse of `expanded` for the first tool found.
func (m *Model) findFirstToolExpandState() bool {
	// Check global tools first
	for _, id := range m.toolOrder {
		if tool := m.tools[id]; tool != nil && tool.status != toolRunning {
			return !tool.expanded
		}
	}
	// Check committed message tools
	for _, msg := range m.messages {
		if msg.role == roleAssistant {
			for _, tool := range msg.tools {
				if tool != nil && tool.status != toolRunning {
					return !tool.expanded
				}
			}
		}
	}
	return false
}

// setAllToolsExpanded sets expanded state for all tools.
func (m *Model) setAllToolsExpanded(expanded bool) {
	for _, id := range m.toolOrder {
		if tool := m.tools[id]; tool != nil && tool.status != toolRunning {
			tool.expanded = expanded
		}
	}

	for idx := range m.messages {
		if m.messages[idx].role == roleAssistant {
			for _, tool := range m.messages[idx].tools {
				if tool != nil && tool.status != toolRunning {
					tool.expanded = expanded
				}
			}
		}
	}
}

// handleHistoryUp navigates to previous prompt in history.
func (m *Model) handleHistoryUp() (tea.Model, tea.Cmd) {
	if m.isStreaming || len(m.history) == 0 {
		return m, nil
	}
	if m.historyIndex == -1 {
		m.savedInput = m.textarea.Value()
		m.historyIndex = len(m.history) - 1
	} else if m.historyIndex > 0 {
		m.historyIndex--
	}

	m.textarea.SetValue(m.history[m.historyIndex])
	m.textarea.CursorEnd()
	return m, nil
}

// handleHistoryDown navigates to next prompt in history or back to current input.
func (m *Model) handleHistoryDown() (tea.Model, tea.Cmd) {
	if m.isStreaming || m.historyIndex < 0 {
		return m, nil
	}
	if m.historyIndex < len(m.history)-1 {
		m.historyIndex++

		m.textarea.SetValue(m.history[m.historyIndex])
	} else {
		m.historyIndex = -1
		m.textarea.SetValue(m.savedInput)
	}
	m.textarea.CursorEnd()
	return m, nil
}

// handleEnter sends the current message.
func (m *Model) handleEnter() (tea.Model, tea.Cmd) {
	if m.isStreaming || strings.TrimSpace(m.textarea.Value()) == "" {
		return m, nil
	}
	content := m.textarea.Value()
	m.textarea.Reset()
	m.isStreaming = true
	m.justCompleted = false
	return m, tea.Batch(m.spinner.Tick, m.sendMessageCmd(content))
}

// handleCopyOutput copies the latest assistant message to clipboard.
// Works when not streaming and there is a completed assistant message.
func (m *Model) handleCopyOutput() (tea.Model, tea.Cmd) {
	// Don't allow copying while streaming
	if m.isStreaming {
		return m, nil
	}

	// Find the last assistant message
	for i := len(m.messages) - 1; i >= 0; i-- {
		if m.messages[i].role == roleAssistant && m.messages[i].content != "" {
			// Copy the raw content (markdown) to clipboard.
			// Silently ignore errors since clipboard may be unavailable in CI/headless environments.
			_ = clipboard.WriteAll(m.messages[i].content)

			// Show feedback and schedule its clearing after 1.5 seconds
			m.showCopyFeedback = true

			return m, tea.Tick(clipboardResetMillis*time.Millisecond, func(_ time.Time) tea.Msg {
				return copyFeedbackClearMsg{}
			})
		}
	}

	return m, nil
}
