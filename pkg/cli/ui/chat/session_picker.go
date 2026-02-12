package chat

import (
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	copilot "github.com/github/copilot-sdk/go"
)

const (
	// maxSessionDisplayLength is the maximum display length for session names in the picker before truncation.
	maxSessionDisplayLength = 30
	// ellipsisLength is the number of characters used for the ellipsis suffix.
	ellipsisLength = 3
)

// handleSessionPickerKey handles keyboard input when the session picker is active.
func (m *Model) handleSessionPickerKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	totalItems := len(m.filteredSessions) + 1 // +1 for "New Chat" option

	// Handle rename mode
	if m.renamingSession {
		return m.handleSessionRenameKey(msg)
	}

	// Handle delete confirmation
	if m.confirmDeleteSession {
		return m.handleSessionDeleteConfirmKey(msg)
	}

	// Handle filter mode
	if m.sessionFilterActive {
		return m.handleSessionFilterKey(msg)
	}

	return m.handleSessionPickerNavKey(msg, totalItems)
}

// handleSessionRenameKey handles keyboard input when renaming a session.
func (m *Model) handleSessionRenameKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case keyEnter:
		return m.confirmSessionRename()
	case keyEscape:
		m.renamingSession = false
		m.sessionRenameInput = ""

		return m, nil
	case keyBackspace:
		if len(m.sessionRenameInput) > 0 {
			m.sessionRenameInput = m.sessionRenameInput[:len(m.sessionRenameInput)-1]
		}

		return m, nil
	case " ":
		m.sessionRenameInput += " "

		return m, nil
	default:
		if msg.Type == tea.KeyRunes {
			m.sessionRenameInput += string(msg.Runes)
		}

		return m, nil
	}
}

// confirmSessionRename saves the renamed session.
func (m *Model) confirmSessionRename() (tea.Model, tea.Cmd) {
	defer func() {
		m.renamingSession = false
		m.sessionRenameInput = ""
	}()

	if m.isInvalidSessionIndex() {
		return m, nil
	}

	session := m.filteredSessions[m.sessionPickerIndex-1]
	newName := strings.TrimSpace(m.sessionRenameInput)
	// Silently ignore empty names (user can press Esc to cancel explicitly)
	if newName == "" {
		return m, nil
	}

	session.Name = newName

	saveErr := SaveSession(&session, m.theme.SessionDir)
	if saveErr != nil {
		m.err = fmt.Errorf("failed to save session: %w", saveErr)

		return m, nil
	}

	refreshErr := m.refreshSessionList()
	if refreshErr != nil {
		return m, nil
	}

	return m, nil
}

// handleSessionDeleteConfirmKey handles keyboard input when confirming session deletion.
func (m *Model) handleSessionDeleteConfirmKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		m.confirmDeleteSession = false

		return m.deleteSelectedSession()
	case "n", "N", "esc":
		m.confirmDeleteSession = false

		return m, nil
	}

	return m, nil
}

// deleteSelectedSession deletes the currently selected session.
func (m *Model) deleteSelectedSession() (tea.Model, tea.Cmd) {
	if m.isInvalidSessionIndex() {
		return m, nil
	}

	session := m.filteredSessions[m.sessionPickerIndex-1]

	deleteErr := DeleteSession(m.client, session.ID, m.theme.SessionDir)
	if deleteErr != nil {
		m.err = fmt.Errorf("failed to delete session: %w", deleteErr)

		return m, nil
	}

	refreshErr := m.refreshSessionList()
	if refreshErr != nil {
		return m, nil
	}

	m.clampSessionIndex()

	return m, nil
}

// refreshSessionList reloads the session list from storage and re-applies filters.
func (m *Model) refreshSessionList() error {
	sessions, err := ListSessions(m.client, m.theme.SessionDir)
	if err != nil {
		m.err = fmt.Errorf("failed to refresh sessions: %w", err)

		return err
	}

	m.availableSessions = sessions
	m.applySessionFilter()

	return nil
}

// handleSessionPickerNavKey handles navigation keys in the session picker.
//
//nolint:cyclop // keyboard dispatcher for session picker navigation
func (m *Model) handleSessionPickerNavKey(msg tea.KeyMsg, totalItems int) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case keyEscape:
		m.showSessionPicker = false
		m.sessionFilterText = ""
		m.sessionFilterActive = false
		m.updateDimensions()

		return m, nil
	case "/":
		m.sessionFilterActive = true

		return m, nil
	case "up", "k":
		if m.sessionPickerIndex > 0 {
			m.sessionPickerIndex--
		}

		return m, nil
	case keyDown, "j":
		if m.sessionPickerIndex < totalItems-1 {
			m.sessionPickerIndex++
		}

		return m, nil
	case "d", "delete", keyBackspace:
		m.initiateSessionDelete()

		return m, nil
	case "r":
		m.initiateSessionRename()

		return m, nil
	case keyEnter:
		return m.selectSession()
	case keyCtrlC:
		m.cleanup()
		m.quitting = true

		return m, tea.Quit
	}

	return m, nil
}

// initiateSessionDelete starts a session deletion confirmation if a valid session is selected.
func (m *Model) initiateSessionDelete() {
	if m.isValidSessionIndex() {
		m.confirmDeleteSession = true
	}
}

// initiateSessionRename starts a session rename if a valid session is selected.
func (m *Model) initiateSessionRename() {
	if m.isValidSessionIndex() {
		session := m.filteredSessions[m.sessionPickerIndex-1]
		m.renamingSession = true
		m.sessionRenameInput = session.GetDisplayName()
	}
}

// handleSessionFilterKey handles keyboard input when filtering sessions.
func (m *Model) handleSessionFilterKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case keyEnter:
		m.sessionFilterActive = false

		return m, nil
	case keyEscape:
		// Clear filter and exit filter mode
		m.sessionFilterText = ""
		m.sessionFilterActive = false
		m.applySessionFilter()

		return m, nil
	case keyBackspace:
		if len(m.sessionFilterText) > 0 {
			m.sessionFilterText = m.sessionFilterText[:len(m.sessionFilterText)-1]
			m.applySessionFilter()
		}

		return m, nil
	case keyCtrlC:
		m.cleanup()
		m.quitting = true

		return m, tea.Quit
	default:
		if msg.Type == tea.KeyRunes {
			m.sessionFilterText += string(msg.Runes)
			m.applySessionFilter()
		}

		return m, nil
	}
}

// applySessionFilter filters the available sessions based on the current filter text.
func (m *Model) applySessionFilter() {
	if m.sessionFilterText == "" {
		m.filteredSessions = m.availableSessions
	} else {
		filterLower := strings.ToLower(m.sessionFilterText)

		m.filteredSessions = make([]SessionMetadata, 0)

		for _, session := range m.availableSessions {
			if strings.Contains(strings.ToLower(session.GetDisplayName()), filterLower) {
				m.filteredSessions = append(m.filteredSessions, session)
			}
		}
	}
	// Reset picker index if it's out of bounds
	maxIndex := len(m.filteredSessions)
	if m.sessionPickerIndex > maxIndex {
		m.sessionPickerIndex = maxIndex
	}
}

// selectSession handles session selection from the picker.
func (m *Model) selectSession() (tea.Model, tea.Cmd) {
	_ = m.saveCurrentSession()

	if m.sessionPickerIndex == 0 {
		startErr := m.startNewSession()
		if startErr != nil {
			m.err = startErr
		}
	} else if m.isValidSessionIndex() {
		session := m.filteredSessions[m.sessionPickerIndex-1]
		m.loadSession(&session)
	}

	m.showSessionPicker = false
	m.sessionFilterText = ""
	m.sessionFilterActive = false
	m.updateDimensions()

	return m, nil
}

// startNewSession destroys the current session and creates a fresh one.
func (m *Model) startNewSession() error {
	m.cleanup()

	if m.session != nil {
		_ = m.session.Destroy()
	}

	m.sessionConfig.SessionID = ""

	session, err := m.client.CreateSession(m.sessionConfig)
	if err != nil {
		return fmt.Errorf("failed to create new session: %w", err)
	}

	m.session = session
	m.currentSessionID = session.SessionID

	// Clear all state for new session
	m.messages = []message{}
	m.agentMode = true // New chats start in agent mode by default
	// Update the shared reference so tool handlers see the change
	if m.agentModeRef != nil {
		m.agentModeRef.SetEnabled(true)
	}

	m.resetStreamingState()
	m.updateViewportContent()

	return nil
}

// loadSession loads a chat session into the model using the Copilot SDK.
func (m *Model) loadSession(metadata *SessionMetadata) {
	m.currentSessionID = metadata.ID
	// Restore mode from session (nil or true = agent mode, false = plan mode)
	m.agentMode = metadata.AgentMode == nil || *metadata.AgentMode
	// Update the shared reference so tool handlers see the change
	if m.agentModeRef != nil {
		m.agentModeRef.SetEnabled(m.agentMode)
	}

	m.cleanup()

	if m.session != nil {
		_ = m.session.Destroy()
	}

	if metadata.Model != "" {
		m.sessionConfig.Model = metadata.Model
		m.currentModel = metadata.Model
	}

	if !m.resumeOrCreateSession(metadata) {
		return
	}

	m.loadSessionMessages(metadata)
	m.resetStreamingState()
	m.updateViewportContent()
}

// resumeOrCreateSession resumes an existing session or creates a new one.
// Returns true if a session was successfully created/resumed.
func (m *Model) resumeOrCreateSession(metadata *SessionMetadata) bool {
	resumeConfig := &copilot.ResumeSessionConfig{
		Tools:               m.sessionConfig.Tools,
		OnPermissionRequest: m.sessionConfig.OnPermissionRequest,
	}

	session, err := m.client.ResumeSessionWithOptions(metadata.ID, resumeConfig)
	if err != nil {
		// If resume fails, try creating a new session with the same ID
		m.sessionConfig.SessionID = metadata.ID

		session, err = m.client.CreateSession(m.sessionConfig)
		if err != nil {
			m.err = fmt.Errorf("failed to resume session: %w", err)

			return false
		}
	}

	m.session = session

	return true
}

// loadSessionMessages loads and renders messages from a resumed session.
func (m *Model) loadSessionMessages(metadata *SessionMetadata) {
	events, err := m.session.GetMessages()
	if err != nil {
		m.messages = []message{}
	} else {
		m.messages = m.sessionEventsToMessages(events, metadata)
	}

	for idx := range m.messages {
		if m.messages[idx].role == roleAssistant && m.messages[idx].content != "" {
			m.messages[idx].rendered = renderMarkdownWithRenderer(
				m.renderer,
				m.messages[idx].content,
			)
		}
	}
}

// resetStreamingState clears streaming-related state without clearing messages.
func (m *Model) resetStreamingState() {
	m.tools = make(map[string]*toolExecution)
	m.toolOrder = make([]string, 0)
	m.currentResponse.Reset()
	m.isStreaming = false
	m.justCompleted = false
	m.pendingToolCount = 0
	m.sessionComplete = false
}

// sessionEventsToMessages converts Copilot SessionEvents to internal messages.
// It also restores per-message metadata (like agentMode) from the session metadata.
func (m *Model) sessionEventsToMessages(
	events []copilot.SessionEvent,
	metadata *SessionMetadata,
) []message {
	var messages []message

	userMsgIndex := 0

	for _, event := range events {
		var role, content string

		//nolint:exhaustive // Only user and assistant messages are relevant for session history.
		switch event.Type {
		case copilot.UserMessage:
			role = roleUser

			if event.Data.Content != nil {
				content = *event.Data.Content
			}
		case copilot.AssistantMessage:
			role = roleAssistant

			if event.Data.Content != nil {
				content = *event.Data.Content
			}
		default:
			continue
		}

		if content != "" {
			msg := message{
				role:    role,
				content: content,
			}
			// Restore agentMode for user messages from metadata
			if role == roleUser {
				if userMsgIndex < len(metadata.Messages) {
					msg.agentMode = metadata.Messages[userMsgIndex].AgentMode
				} else {
					// Default to true (agent mode) for messages without metadata
					msg.agentMode = true
				}

				userMsgIndex++
			}

			messages = append(messages, msg)
		}
	}

	return messages
}

// saveCurrentSession saves the current session metadata to disk.
func (m *Model) saveCurrentSession() error {
	if len(m.messages) == 0 || m.session == nil {
		return nil
	}

	sessionID := m.session.SessionID
	if sessionID == "" {
		return nil
	}

	// Preserve existing custom name if set
	name := GenerateSessionName(m.messages)

	existing, loadErr := LoadSession(sessionID, m.theme.SessionDir)
	if loadErr == nil && existing.Name != "" {
		// Keep user-defined names (anything that differs from auto-generated)
		name = existing.Name
	}

	agentMode := m.agentMode
	// Build per-message metadata
	messageMetadata := make([]MessageMetadata, 0)

	for _, msg := range m.messages {
		if msg.role == roleUser {
			messageMetadata = append(messageMetadata, MessageMetadata{
				AgentMode: msg.agentMode,
			})
		}
	}

	metadata := &SessionMetadata{
		ID:        sessionID,
		Messages:  messageMetadata,
		Model:     m.currentModel,
		Name:      name,
		AgentMode: &agentMode,
	}

	saveErr := SaveSession(metadata, m.theme.SessionDir)
	if saveErr != nil {
		return saveErr
	}

	m.currentSessionID = sessionID

	return nil
}

// renderSessionPickerModal renders the session picker as an inline modal section.
func (m *Model) renderSessionPickerModal() string {
	modalWidth := max(m.width-modalPadding, 1)
	contentWidth := max(modalWidth-contentPadding, 1)
	clipStyle := lipgloss.NewStyle().MaxWidth(contentWidth).Inline(true)

	totalItems := len(m.filteredSessions) + 1
	maxVisible := m.calculateMaxPickerVisible()
	visibleCount := min(totalItems, maxVisible)

	scrollOffset := calculatePickerScrollOffset(m.sessionPickerIndex, totalItems, maxVisible)

	var listContent strings.Builder
	m.renderSessionPickerTitle(&listContent, clipStyle)

	isScrollable := totalItems > maxVisible
	m.renderScrollIndicatorTop(&listContent, clipStyle, isScrollable, scrollOffset)

	endIdx := min(scrollOffset+visibleCount, totalItems)
	m.renderSessionItems(&listContent, clipStyle, scrollOffset, endIdx)

	m.renderScrollIndicatorBottom(&listContent, clipStyle, isScrollable, endIdx, totalItems)

	content := strings.TrimRight(listContent.String(), "\n")

	return m.renderPickerModal(content, modalWidth, visibleCount, isScrollable)
}

// renderSessionPickerTitle renders the title, filter input, or delete confirmation.
func (m *Model) renderSessionPickerTitle(listContent *strings.Builder, clipStyle lipgloss.Style) {
	if m.confirmDeleteSession && m.isValidSessionIndex() {
		session := m.filteredSessions[m.sessionPickerIndex-1]
		listContent.WriteString(clipStyle.Render(
			lipgloss.NewStyle().Foreground(lipgloss.ANSIColor(ansiYellow)).Render(
				fmt.Sprintf("Delete \"%s\"?", truncateString(session.GetDisplayName(), maxSessionDisplayLength)),
			),
		) + "\n\n")

		return
	}

	// Show filter input if filtering is active or has text
	if m.sessionFilterActive || m.sessionFilterText != "" {
		filterStyle := lipgloss.NewStyle().Foreground(lipgloss.ANSIColor(ansiCyan))

		cursor := ""
		if m.sessionFilterActive {
			cursor = "_"
		}

		filterLine := filterStyle.Render("🔍 " + m.sessionFilterText + cursor)
		listContent.WriteString(clipStyle.Render(filterLine) + "\n")
	} else {
		listContent.WriteString(clipStyle.Render("Chat History") + "\n")
	}
}

// renderSessionItems renders the visible session list items.
func (m *Model) renderSessionItems(
	listContent *strings.Builder,
	clipStyle lipgloss.Style,
	scrollOffset, endIdx int,
) {
	for i := scrollOffset; i < endIdx; i++ {
		line, isCurrentSession := m.formatSessionItem(i)
		styledLine := m.styleSessionItem(line, i, isCurrentSession)
		listContent.WriteString(clipStyle.Render(styledLine) + "\n")
	}
}

// formatSessionItem formats a single session item for display.
func (m *Model) formatSessionItem(index int) (string, bool) {
	prefix := "  "
	if index == m.sessionPickerIndex {
		prefix = "> "
	}

	if index == 0 {
		return prefix + "+ New Chat", m.currentSessionID == ""
	}

	session := m.filteredSessions[index-1]

	if m.renamingSession && index == m.sessionPickerIndex {
		inputStyle := lipgloss.NewStyle().
			Foreground(lipgloss.ANSIColor(ansiWhite)).
			Background(lipgloss.ANSIColor(ansiGray))

		line := prefix + inputStyle.Render(m.sessionRenameInput+"_")
		if session.ID == m.currentSessionID {
			line += checkmarkSuffix
		}

		return line, session.ID == m.currentSessionID
	}

	timeAgo := m.formatSessionTime(&session)
	name := truncateString(session.GetDisplayName(), maxSessionDisplayLength)
	line := fmt.Sprintf("%s%s (%s)", prefix, name, timeAgo)

	isCurrentSession := session.ID == m.currentSessionID
	if isCurrentSession {
		line += checkmarkSuffix
	}

	return line, isCurrentSession
}

// styleSessionItem applies styling to a session item.
func (m *Model) styleSessionItem(line string, index int, isCurrentSession bool) string {
	if m.renamingSession && index == m.sessionPickerIndex && index > 0 {
		return line
	}

	if index == m.sessionPickerIndex {
		return lipgloss.NewStyle().
			Foreground(lipgloss.ANSIColor(ansiCyan)).
			Bold(true).
			Render(line)
	}

	if isCurrentSession {
		return lipgloss.NewStyle().
			Foreground(lipgloss.ANSIColor(ansiGreen)).
			Render(line)
	}

	return line
}

// formatSessionTime returns a formatted relative time for a session.
// Uses SDK's ModifiedTime if available, otherwise falls back to local UpdatedAt.
func (m *Model) formatSessionTime(session *SessionMetadata) string {
	if session.SDKMetadata != nil && session.SDKMetadata.ModifiedTime != "" {
		// Parse SDK's ISO timestamp
		t, err := time.Parse(time.RFC3339, session.SDKMetadata.ModifiedTime)
		if err == nil {
			return FormatRelativeTime(t)
		}
	}
	// Fall back to local UpdatedAt
	return FormatRelativeTime(session.UpdatedAt)
}

// truncateString truncates a string to maxLen runes with ellipsis (Unicode-safe).
func truncateString(s string, maxLen int) string {
	if utf8.RuneCountInString(s) <= maxLen {
		return s
	}

	runes := []rune(s)

	return string(runes[:maxLen-ellipsisLength]) + "..."
}

// isValidSessionIndex returns true if the picker index points to a valid session (not "New Chat").
func (m *Model) isValidSessionIndex() bool {
	return m.sessionPickerIndex > 0 && m.sessionPickerIndex <= len(m.filteredSessions)
}

// isInvalidSessionIndex returns true if the picker index is out of bounds for any selection.
func (m *Model) isInvalidSessionIndex() bool {
	return m.sessionPickerIndex <= 0 || m.sessionPickerIndex > len(m.filteredSessions)
}

// clampSessionIndex ensures the session picker index is within valid bounds.
func (m *Model) clampSessionIndex() {
	if m.sessionPickerIndex > len(m.filteredSessions) {
		m.sessionPickerIndex = len(m.filteredSessions)
	}
}
