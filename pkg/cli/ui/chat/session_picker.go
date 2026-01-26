package chat

import (
	"fmt"
	"strings"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	copilot "github.com/github/copilot-sdk/go"
)

// handleSessionPickerKey handles keyboard input when the session picker is active.
func (m *Model) handleSessionPickerKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	totalItems := len(m.availableSessions) + 1 // +1 for "New Chat" option

	// Handle rename mode
	if m.renamingSession {
		return m.handleSessionRenameKey(msg)
	}

	// Handle delete confirmation
	if m.confirmDeleteSession {
		return m.handleSessionDeleteConfirmKey(msg)
	}

	return m.handleSessionPickerNavKey(msg, totalItems)
}

// handleSessionRenameKey handles keyboard input when renaming a session.
func (m *Model) handleSessionRenameKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		return m.confirmSessionRename()
	case "esc":
		m.renamingSession = false
		m.sessionRenameInput = ""
		return m, nil
	case "backspace":
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
	if m.sessionPickerIndex > 0 && m.sessionPickerIndex <= len(m.availableSessions) {
		session := m.availableSessions[m.sessionPickerIndex-1]
		newName := strings.TrimSpace(m.sessionRenameInput)
		if newName != "" {
			session.Name = newName
			_ = SaveSession(&session)
			sessions, _ := ListSessions()
			m.availableSessions = sessions
		}
	}
	m.renamingSession = false
	m.sessionRenameInput = ""
	return m, nil
}

// handleSessionDeleteConfirmKey handles keyboard input when confirming session deletion.
func (m *Model) handleSessionDeleteConfirmKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		if m.sessionPickerIndex > 0 && m.sessionPickerIndex <= len(m.availableSessions) {
			session := m.availableSessions[m.sessionPickerIndex-1]
			_ = DeleteSession(session.ID)
			sessions, _ := ListSessions()
			m.availableSessions = sessions
			if m.sessionPickerIndex > len(m.availableSessions) {
				m.sessionPickerIndex = len(m.availableSessions)
			}
		}
		m.confirmDeleteSession = false
		return m, nil
	case "n", "N", "esc":
		m.confirmDeleteSession = false
		return m, nil
	}
	return m, nil
}

// handleSessionPickerNavKey handles navigation keys in the session picker.
func (m *Model) handleSessionPickerNavKey(msg tea.KeyMsg, totalItems int) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.showSessionPicker = false
		m.updateDimensions()
		return m, nil
	case "up", "k":
		if m.sessionPickerIndex > 0 {
			m.sessionPickerIndex--
		}
		return m, nil
	case "down", "j":
		if m.sessionPickerIndex < totalItems-1 {
			m.sessionPickerIndex++
		}
		return m, nil
	case "d", "delete", "backspace":
		if m.sessionPickerIndex > 0 && m.sessionPickerIndex <= len(m.availableSessions) {
			m.confirmDeleteSession = true
		}
		return m, nil
	case "r":
		if m.sessionPickerIndex > 0 && m.sessionPickerIndex <= len(m.availableSessions) {
			session := m.availableSessions[m.sessionPickerIndex-1]
			m.renamingSession = true
			m.sessionRenameInput = session.Name
		}
		return m, nil
	case "enter":
		return m.selectSession()
	case "ctrl+c":
		m.cleanup()
		m.quitting = true
		return m, tea.Quit
	}
	return m, nil
}

// selectSession handles session selection from the picker.
func (m *Model) selectSession() (tea.Model, tea.Cmd) {
	_ = m.saveCurrentSession()
	if m.sessionPickerIndex == 0 {
		if err := m.startNewSession(); err != nil {
			m.err = err
		}
	} else if m.sessionPickerIndex > 0 && m.sessionPickerIndex <= len(m.availableSessions) {
		session := m.availableSessions[m.sessionPickerIndex-1]
		m.loadSession(&session)
	}
	m.showSessionPicker = false
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
	m.messages = []chatMessage{}
	m.resetStreamingState()
	m.updateViewportContent()
	return nil
}

// loadSession loads a chat session into the model using the Copilot SDK.
func (m *Model) loadSession(metadata *SessionMetadata) {
	m.currentSessionID = metadata.ID

	m.cleanup()
	if m.session != nil {
		_ = m.session.Destroy()
	}

	if metadata.Model != "" {
		m.sessionConfig.Model = metadata.Model
		m.currentModel = metadata.Model
	}

	session, err := m.client.ResumeSession(metadata.ID)
	if err != nil {
		m.sessionConfig.SessionID = metadata.ID
		session, err = m.client.CreateSession(m.sessionConfig)
		if err != nil {
			m.err = fmt.Errorf("failed to resume session: %w", err)
			return
		}
	}
	m.session = session

	events, err := session.GetMessages()
	if err != nil {
		m.messages = []chatMessage{}
	} else {
		m.messages = m.sessionEventsToMessages(events)
	}

	for i := range m.messages {
		if m.messages[i].role == "assistant" && m.messages[i].content != "" {
			m.messages[i].rendered = renderMarkdownWithRenderer(m.renderer, m.messages[i].content)
		}
	}

	m.resetStreamingState()
	m.updateViewportContent()
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

// sessionEventsToMessages converts Copilot SessionEvents to internal chatMessages.
func (m *Model) sessionEventsToMessages(events []copilot.SessionEvent) []chatMessage {
	var messages []chatMessage
	for _, event := range events {
		var role, content string
		switch event.Type {
		case copilot.UserMessage:
			role = "user"
			if event.Data.Content != nil {
				content = *event.Data.Content
			}
		case copilot.AssistantMessage:
			role = "assistant"
			if event.Data.Content != nil {
				content = *event.Data.Content
			}
		default:
			continue
		}
		if content != "" {
			messages = append(messages, chatMessage{
				role:    role,
				content: content,
			})
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

	metadata := &SessionMetadata{
		ID:    sessionID,
		Model: m.currentModel,
		Name:  GenerateSessionName(m.messages),
	}

	if err := SaveSession(metadata); err != nil {
		return err
	}

	m.currentSessionID = sessionID
	return nil
}

// renderSessionPickerModal renders the session picker as an inline modal section.
func (m *Model) renderSessionPickerModal() string {
	modalWidth := m.width - 2
	contentWidth := max(modalWidth-4, 1)
	clipStyle := lipgloss.NewStyle().MaxWidth(contentWidth).Inline(true)

	totalItems := len(m.availableSessions) + 1
	const maxVisible = 3
	visibleCount := min(totalItems, maxVisible)

	scrollOffset := calculatePickerScrollOffset(m.sessionPickerIndex, totalItems, maxVisible)

	var listContent strings.Builder
	m.renderSessionPickerTitle(&listContent, clipStyle)

	isScrollable := totalItems > maxVisible
	renderScrollIndicatorTop(&listContent, clipStyle, isScrollable, scrollOffset)

	endIdx := min(scrollOffset+visibleCount, totalItems)
	m.renderSessionItems(&listContent, clipStyle, scrollOffset, endIdx)

	renderScrollIndicatorBottom(&listContent, clipStyle, isScrollable, endIdx, totalItems)

	content := strings.TrimRight(listContent.String(), "\n")
	return renderPickerModal(content, modalWidth, visibleCount, isScrollable)
}

// renderSessionPickerTitle renders the title or delete confirmation.
func (m *Model) renderSessionPickerTitle(listContent *strings.Builder, clipStyle lipgloss.Style) {
	if m.confirmDeleteSession && m.sessionPickerIndex > 0 &&
		m.sessionPickerIndex <= len(m.availableSessions) {
		session := m.availableSessions[m.sessionPickerIndex-1]
		listContent.WriteString(clipStyle.Render(
			lipgloss.NewStyle().Foreground(lipgloss.ANSIColor(11)).Render(
				fmt.Sprintf("Delete \"%s\"?", truncateString(session.Name, 30)),
			),
		) + "\n\n")
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

	session := m.availableSessions[index-1]

	if m.renamingSession && index == m.sessionPickerIndex {
		inputStyle := lipgloss.NewStyle().
			Foreground(lipgloss.ANSIColor(15)).
			Background(lipgloss.ANSIColor(8))
		line := prefix + inputStyle.Render(m.sessionRenameInput+"_")
		if session.ID == m.currentSessionID {
			line += " ✓"
		}
		return line, session.ID == m.currentSessionID
	}

	timeAgo := FormatRelativeTime(session.UpdatedAt)
	name := truncateString(session.Name, 30)
	line := fmt.Sprintf("%s%s (%s)", prefix, name, timeAgo)
	isCurrentSession := session.ID == m.currentSessionID
	if isCurrentSession {
		line += " ✓"
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
			Foreground(lipgloss.ANSIColor(14)).
			Bold(true).
			Render(line)
	}
	if isCurrentSession {
		return lipgloss.NewStyle().
			Foreground(lipgloss.ANSIColor(10)).
			Render(line)
	}
	return line
}

// truncateString truncates a string to maxLen runes with ellipsis (Unicode-safe).
func truncateString(s string, maxLen int) string {
	if utf8.RuneCountInString(s) <= maxLen {
		return s
	}
	runes := []rune(s)
	return string(runes[:maxLen-3]) + "..."
}
