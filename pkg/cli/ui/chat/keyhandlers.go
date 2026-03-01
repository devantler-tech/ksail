package chat

import (
	"strings"
	"time"

	"github.com/atotto/clipboard"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/github/copilot-sdk/go/rpc"
)

const (
	// feedbackResetMillis is the delay before clearing transient UI feedback
	// (e.g., copy confirmation, model-unavailable notice).
	feedbackResetMillis = 1500
)

// handleKeyMsg handles keyboard input.
func (m *Model) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Handle help overlay with highest priority (only F1 or esc can close it)
	if m.showHelpOverlay {
		return m.handleHelpOverlayKey(msg)
	}

	// Handle exit confirmation (before other overlays)
	if m.confirmExit {
		return m.handleExitConfirmKey(msg)
	}

	// Handle overlays first (highest priority)
	if m.pendingPermission != nil {
		return m.handlePermissionKey(msg)
	}

	if m.showModelPicker {
		return m.handleModelPickerKey(msg)
	}

	if m.showReasoningPicker {
		return m.handleReasoningPickerKey(msg)
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
	case keyEnter:
		return m.handleEnter()
	case "ctrl+q":
		return m.handleQueuePrompt()
	case "ctrl+d":
		return m.handleDeletePendingPrompt()
	}

	return m.handleChatShortcutKey(msg)
}

// handleChatShortcutKey handles shortcut keys (toggles, pickers, help).
func (m *Model) handleChatShortcutKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "f1":
		m.showHelpOverlay = true

		return m, nil
	case "ctrl+o":
		return m.handleOpenModelPicker()
	case "ctrl+e":
		return m.handleOpenReasoningPicker()
	case "ctrl+h":
		return m.handleOpenSessionPicker()
	case "ctrl+n":
		return m.handleNewChat()
	case "tab":
		return m.handleToggleMode()
	case "ctrl+t":
		return m.handleToggleAllTools()
	case "ctrl+y":
		return m.handleToggleYolo()
	case "ctrl+r":
		return m.handleCopyOutput()
	}

	return m.handleNavigationKey(msg)
}

// handleNavigationKey handles history navigation and viewport/textarea input.
func (m *Model) handleNavigationKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up":
		return m.handleHistoryUp()
	case keyDown:
		return m.handleHistoryDown()
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

	m.confirmExit = true

	return m, nil
}

// handleExitConfirmKey handles keyboard input on the exit confirmation modal.
func (m *Model) handleExitConfirmKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		return m.handleQuit(true)
	case "n", "N", keyEscape:
		m.confirmExit = false

		return m, nil
	}

	return m, nil
}

// handleOpenModelPicker opens the model selection picker.
// If models have not been loaded yet, it attempts to fetch them lazily
// (matching the session picker's on-demand pattern). If the fetch fails
// or returns an empty list, transient feedback is shown to the user.
func (m *Model) handleOpenModelPicker() (tea.Model, tea.Cmd) {
	if m.isStreaming {
		return m, nil
	}

	// Lazy-load models on first use (or retry after a previous failure)
	if len(m.availableModels) == 0 {
		allModels, err := m.client.ListModels(m.ctx)
		if err != nil {
			m.modelUnavailableReason = err.Error()
		} else {
			m.modelUnavailableReason = ""
			m.availableModels = FilterEnabledModels(allModels)
		}
	}

	if len(m.availableModels) == 0 {
		m.showModelUnavailableFeedback = true

		return m, tea.Tick(feedbackResetMillis*time.Millisecond, func(_ time.Time) tea.Msg {
			return modelUnavailableClearMsg{}
		})
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
// When auto mode is active, returns 0 (the auto option) regardless of
// which model the server resolved to.
func (m *Model) findCurrentModelIndex() int {
	if m.isAutoMode() {
		return 0
	}

	for idx, model := range m.filteredModels {
		if model.ID == m.currentModel {
			return idx + 1 // offset by 1 for auto option
		}
	}

	return 0
}

// handleOpenReasoningPicker opens the reasoning effort selection picker.
// Only opens when not streaming. Shows the picker with the current effort pre-selected.
func (m *Model) handleOpenReasoningPicker() (tea.Model, tea.Cmd) {
	if m.isStreaming {
		return m, nil
	}

	m.showReasoningPicker = true
	m.updateDimensions()
	m.reasoningPickerIndex = m.findCurrentReasoningIndex()

	return m, nil
}

// handleOpenSessionPicker opens the session history picker.
func (m *Model) handleOpenSessionPicker() (tea.Model, tea.Cmd) {
	if m.isStreaming {
		return m, nil
	}

	sessions, _ := ListSessions(m.ctx, m.client, m.theme.SessionDir)
	m.availableSessions = sessions
	m.filteredSessions = sessions // Start with all sessions
	m.sessionFilterText = ""      // Reset filter
	m.sessionFilterActive = false // Start in navigation mode
	m.showSessionPicker = true
	m.confirmDeleteSession = false
	m.confirmExit = false
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

// handleToggleMode cycles through chat modes: Agent -> Plan -> Agent.
func (m *Model) handleToggleMode() (tea.Model, tea.Cmd) {
	// Prevent mode toggling while streaming to avoid mode mismatch between
	// message submission and tool execution time
	if m.isStreaming {
		return m, nil
	}

	m.chatMode = m.chatMode.Next()
	// Update the shared reference so tool handlers see the change
	if m.chatModeRef != nil {
		m.chatModeRef.SetMode(m.chatMode)
	}

	// Notify the Copilot CLI server of the mode change so it can enforce
	// mode-specific behavior (e.g., blocking tools in plan mode).
	if m.session != nil {
		_, _ = m.session.RPC.Mode.Set(m.ctx, &rpc.SessionModeSetParams{
			Mode: m.chatMode.ToSDKMode(),
		})
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

// handleEnter sends the current message, or steers if streaming.
func (m *Model) handleEnter() (tea.Model, tea.Cmd) {
	if strings.TrimSpace(m.textarea.Value()) == "" {
		return m, nil
	}

	if m.isStreaming {
		return m.handleSteerPrompt()
	}

	content := m.textarea.Value()
	m.textarea.Reset()
	m.isStreaming = true
	m.justCompleted = false

	return m, tea.Batch(m.spinner.Tick, m.sendMessageCmd(content))
}

// handleToggleYolo toggles YOLO mode (auto-approve write operations).
func (m *Model) handleToggleYolo() (tea.Model, tea.Cmd) {
	// Prevent toggling while streaming to avoid state mismatch
	if m.isStreaming {
		return m, nil
	}

	m.yoloMode = !m.yoloMode
	// Update the shared reference so tool handlers see the change
	if m.yoloModeRef != nil {
		m.yoloModeRef.SetEnabled(m.yoloMode)
	}

	m.updateViewportContent()

	return m, nil
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

			return m, tea.Tick(feedbackResetMillis*time.Millisecond, func(_ time.Time) tea.Msg {
				return copyFeedbackClearMsg{}
			})
		}
	}

	return m, nil
}

// handleQueuePrompt adds the current textarea content as a queued prompt.
// Queued prompts are processed in FIFO order after the current prompt completes.
// Only available while streaming.
func (m *Model) handleQueuePrompt() (tea.Model, tea.Cmd) {
	if !m.isStreaming {
		return m, nil
	}

	content := strings.TrimSpace(m.textarea.Value())
	if content == "" {
		return m, nil
	}

	m.addQueuedPrompt(content)
	m.textarea.Reset()
	m.updateViewportContent()

	return m, nil
}

// handleSteerPrompt adds the current textarea content as a steering prompt.
// Steering prompts are injected as soon as the session becomes idle,
// allowing the user to provide guidance while a task is running.
func (m *Model) handleSteerPrompt() (tea.Model, tea.Cmd) {
	content := strings.TrimSpace(m.textarea.Value())
	if content == "" {
		return m, nil
	}

	m.addSteeringPrompt(content)
	m.textarea.Reset()
	m.updateViewportContent()

	return m, nil
}

// handleDeletePendingPrompt removes the last queued prompt if any exist;
// if there are no queued prompts, it removes the last steering prompt.
func (m *Model) handleDeletePendingPrompt() (tea.Model, tea.Cmd) {
	if !m.hasPendingPrompts() {
		return m, nil
	}

	m.deleteLastPendingPrompt()
	m.updateViewportContent()

	return m, nil
}
