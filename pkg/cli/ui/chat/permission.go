package chat

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// handlePermissionRequest handles incoming permission request messages.
func (m *Model) handlePermissionRequest(req *permissionRequestMsg) (tea.Model, tea.Cmd) {
	m.pendingPermission = req
	m.updateDimensions()
	m.updateViewportContent()

	return m, nil
}

// handlePermissionKey handles keyboard input when a permission prompt is active.
func (m *Model) handlePermissionKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.pendingPermission == nil {
		return m, nil
	}

	switch msg.String() {
	case "y", "Y":
		return m.allowPermission()
	case "n", "N", "esc":
		return m.denyPermission()
	case "ctrl+c":
		m.pendingPermission.response <- false

		m.pendingPermission = nil
		m.cleanup()
		m.quitting = true

		return m, tea.Quit
	}

	return m, nil
}

// allowPermission approves the pending permission request.
func (m *Model) allowPermission() (tea.Model, tea.Cmd) {
	m.permissionHistory = append(m.permissionHistory, permissionResponse{
		toolName: m.pendingPermission.toolName,
		command:  m.pendingPermission.command,
		allowed:  true,
	})
	m.pendingPermission.response <- true

	m.pendingPermission = nil
	m.updateDimensions()
	m.updateViewportContent()

	return m, m.waitForEvent()
}

// denyPermission denies the pending permission request.
func (m *Model) denyPermission() (tea.Model, tea.Cmd) {
	m.permissionHistory = append(m.permissionHistory, permissionResponse{
		toolName: m.pendingPermission.toolName,
		command:  m.pendingPermission.command,
		allowed:  false,
	})
	m.pendingPermission.response <- false

	m.pendingPermission = nil
	m.updateDimensions()
	m.updateViewportContent()

	return m, m.waitForEvent()
}

// renderPermissionModal renders the permission prompt as an inline modal section.
func (m *Model) renderPermissionModal() string {
	if m.pendingPermission == nil {
		return ""
	}

	modalWidth := m.width - modalPadding
	contentWidth := max(modalWidth-contentPadding, 1)
	clipStyle := lipgloss.NewStyle().MaxWidth(contentWidth).Inline(true)
	warningStyle := lipgloss.NewStyle().Foreground(lipgloss.ANSIColor(ansiYellow))

	var content strings.Builder

	contentLines := 0

	content.WriteString(clipStyle.Render(warningStyle.Render("⚠️  Permission Required")) + "\n\n")

	contentLines += 2

	humanName := humanizeToolName(m.pendingPermission.toolName)
	content.WriteString(clipStyle.Render("Tool: "+humanName) + "\n")

	contentLines++

	if m.pendingPermission.command != "" {
		content.WriteString(
			clipStyle.Render("Command: "+m.pendingPermission.command) + "\n",
		)

		contentLines++
	}

	if m.pendingPermission.arguments != "" {
		content.WriteString(
			clipStyle.Render("Arguments: "+m.pendingPermission.arguments) + "\n",
		)

		contentLines++
	}

	content.WriteString("\n" + clipStyle.Render("Allow this operation?") + "\n")

	contentLines += 3

	modalStyle := lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.ANSIColor(ansiYellow)).
		PaddingLeft(1).
		PaddingRight(1).
		Width(modalWidth).
		Height(contentLines)

	return modalStyle.Render(strings.TrimRight(content.String(), "\n"))
}
