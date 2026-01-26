package chat

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// handleKeyMsg handles keyboard input.
func (m *Model) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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

	// Handle main key events
	switch msg.String() {
	case "ctrl+c":
		return m.handleQuit(true)
	case "esc":
		return m.handleEscape()
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
		return m.handleToggleCurrentTools()
	case "ctrl+t":
		return m.handleToggleAllTools()
	case "up":
		return m.handleHistoryUp()
	case "down":
		return m.handleHistoryDown()
	case "enter":
		return m.handleEnter()
	}

	// Handle page up/down for viewport scrolling
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
		if len(m.messages) > 0 && m.messages[len(m.messages)-1].role == "assistant" {
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
	m.updateDimensions()
	m.modelPickerIndex = m.findCurrentModelIndex()
	return m, nil
}

// findCurrentModelIndex returns the picker index for the current model.
func (m *Model) findCurrentModelIndex() int {
	if m.currentModel == "" || m.currentModel == "auto" {
		return 0
	}
	for i, model := range m.availableModels {
		if model.ID == m.currentModel {
			return i + 1 // offset by 1 for auto option
		}
	}
	return 0
}

// handleOpenSessionPicker opens the session history picker.
func (m *Model) handleOpenSessionPicker() (tea.Model, tea.Cmd) {
	if m.isStreaming {
		return m, nil
	}
	sessions, _ := ListSessions()
	m.availableSessions = sessions
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
	for i, session := range m.availableSessions {
		if session.ID == m.currentSessionID {
			return i + 1 // offset by 1 for "New Chat" option
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
	if err := m.startNewSession(); err != nil {
		m.err = err
	}
	return m, nil
}

// handleToggleCurrentTools toggles tool expansion for the current message only.
func (m *Model) handleToggleCurrentTools() (tea.Model, tea.Cmd) {
	toggled := m.toggleGlobalTools()
	if !toggled {
		m.toggleLastMessageTools()
	}
	m.updateViewportContent()
	return m, nil
}

// toggleGlobalTools toggles tools in the global tool map (current streaming message).
func (m *Model) toggleGlobalTools() bool {
	if len(m.toolOrder) == 0 {
		return false
	}
	expandAll := m.findFirstToolExpandState(true)
	for _, id := range m.toolOrder {
		if tool := m.tools[id]; tool != nil && tool.status != toolRunning {
			tool.expanded = expandAll
		}
	}
	return true
}

// toggleLastMessageTools toggles tools in the last assistant message.
func (m *Model) toggleLastMessageTools() {
	for i := len(m.messages) - 1; i >= 0; i-- {
		if m.messages[i].role == "assistant" && len(m.messages[i].tools) > 0 {
			expandAll := !m.messages[i].tools[0].expanded
			for _, tool := range m.messages[i].tools {
				if tool != nil && tool.status != toolRunning {
					tool.expanded = expandAll
				}
			}
			break
		}
	}
}

// handleToggleAllTools toggles tool expansion for all messages.
func (m *Model) handleToggleAllTools() (tea.Model, tea.Cmd) {
	expandAll := m.findFirstToolExpandState(false)
	m.setAllToolsExpanded(expandAll)
	m.updateViewportContent()
	return m, nil
}

// findFirstToolExpandState finds the expand state of the first tool.
// Returns the inverse of `expanded` for the first tool found.
func (m *Model) findFirstToolExpandState(globalOnly bool) bool {
	// Check global tools first
	for _, id := range m.toolOrder {
		if tool := m.tools[id]; tool != nil && tool.status != toolRunning {
			return !tool.expanded
		}
	}
	if globalOnly {
		return false
	}
	// Check committed message tools
	for _, msg := range m.messages {
		if msg.role == "assistant" {
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
	for i := range m.messages {
		if m.messages[i].role == "assistant" {
			for _, tool := range m.messages[i].tools {
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
