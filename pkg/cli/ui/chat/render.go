package chat

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// renderHeader renders the header section with logo and status.
func (m *Model) renderHeader() string {
	headerContentWidth := max(m.width-headerPadding, 1)

	// Truncate each logo line by display width (handles Unicode properly)
	logoLines := strings.Split(logo(), "\n")
	truncateStyle := lipgloss.NewStyle().MaxWidth(headerContentWidth).Inline(true)

	var clippedLogo strings.Builder

	for idx, line := range logoLines {
		clippedLine := truncateStyle.Render(line)
		clippedLogo.WriteString(clippedLine)

		if idx < len(logoLines)-1 {
			clippedLogo.WriteString("\n")
		}
	}

	logoRendered := logoStyle.Render(clippedLogo.String())

	// Build tagline with right-aligned status
	taglineRow := m.buildTaglineRow(headerContentWidth)
	taglineRow = lipgloss.NewStyle().MaxWidth(headerContentWidth).Inline(true).Render(taglineRow)

	headerContent := logoRendered + "\n" + taglineRow

	return headerBoxStyle.Width(m.width - modalPadding).Render(headerContent)
}

// buildTaglineRow builds the tagline row with right-aligned status indicator.
func (m *Model) buildTaglineRow(contentWidth int) string {
	taglineText := taglineStyle.Render("  " + tagline())
	statusText := m.buildStatusText()

	if statusText == "" {
		return taglineText
	}

	taglineLen := lipgloss.Width(taglineText)
	statusLen := lipgloss.Width(statusText)
	spacing := max(contentWidth-taglineLen-statusLen, minSpacing)

	return taglineText + strings.Repeat(" ", spacing) + statusText
}

// buildStatusText builds the status indicator text (mode, model, streaming state).
func (m *Model) buildStatusText() string {
	var statusParts []string

	// Mode icon: </> for Agent, ≡ for Plan
	modeStyle := lipgloss.NewStyle().Foreground(lipgloss.ANSIColor(ansiCyan))
	if m.agentMode {
		statusParts = append(statusParts, modeStyle.Render("</>"))
	} else {
		statusParts = append(statusParts, modeStyle.Render("≡"))
	}

	// Model name
	modelStyle := lipgloss.NewStyle().Foreground(dimColor)

	switch {
	case m.currentModel != "":
		statusParts = append(statusParts, modelStyle.Render(m.currentModel))
	default:
		statusParts = append(statusParts, modelStyle.Render(modelAuto))
	}

	// Streaming state and feedback
	switch {
	case m.isStreaming:
		statusParts = append(statusParts, m.spinner.View()+" "+statusStyle.Render("Thinking..."))
	case m.showCopyFeedback:
		statusParts = append(statusParts, statusStyle.Render("Copied ✓"))
	case m.justCompleted:
		statusParts = append(statusParts, statusStyle.Render("Ready ✓"))
	}

	return strings.Join(statusParts, " • ")
}

// renderInputOrModal renders either the input area or active modal.
func (m *Model) renderInputOrModal() string {
	if m.showHelpOverlay {
		return m.renderHelpOverlay()
	}

	if m.pendingPermission != nil {
		return m.renderPermissionModal()
	}

	if m.showModelPicker {
		return m.renderModelPickerModal()
	}

	if m.showSessionPicker {
		return m.renderSessionPickerModal()
	}

	return inputStyle.Width(m.width - modalPadding).Render(m.textarea.View())
}

// renderFooter renders the context-aware help text footer using bubbles/help.
func (m *Model) renderFooter() string {
	return lipgloss.NewStyle().MaxWidth(m.width).Inline(true).Render(m.renderShortHelp())
}
