package chat

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// renderHeader renders the header section with logo and status.
func (m *Model) renderHeader() string {
	headerContentWidth := max(m.width-headerPadding, 1)

	// Truncate each logo line by display width (handles Unicode properly)
	logoLines := strings.Split(m.theme.Logo(), "\n")
	truncateStyle := lipgloss.NewStyle().MaxWidth(headerContentWidth).Inline(true)

	var clippedLogo strings.Builder

	for idx, line := range logoLines {
		clippedLine := truncateStyle.Render(line)
		clippedLogo.WriteString(clippedLine)

		if idx < len(logoLines)-1 {
			clippedLogo.WriteString("\n")
		}
	}

	logoRendered := m.styles.logo.Render(clippedLogo.String())

	// Build tagline with right-aligned status
	taglineRow := m.buildTaglineRow(headerContentWidth)
	taglineRow = lipgloss.NewStyle().MaxWidth(headerContentWidth).Inline(true).Render(taglineRow)

	headerContent := logoRendered + "\n" + taglineRow

	return m.styles.headerBox.Width(max(m.width-modalPadding, 1)).Render(headerContent)
}

// buildTaglineRow builds the tagline row with right-aligned status indicator.
func (m *Model) buildTaglineRow(contentWidth int) string {
	taglineText := m.styles.tagline.Render("  " + m.theme.Tagline())
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

	// Mode indicator with icon and label
	modeStyle := lipgloss.NewStyle().Foreground(lipgloss.ANSIColor(ansiCyan))
	statusParts = append(statusParts, modeStyle.Render(m.chatMode.Label()))

	// Auto-approve indicator
	if m.yoloMode {
		yoloStyle := lipgloss.NewStyle().Foreground(lipgloss.ANSIColor(ansiYellow)).Bold(true)
		statusParts = append(statusParts, yoloStyle.Render("auto-approve"))
	}

	// Model name — show "auto → resolved" when in auto mode, otherwise the explicit model
	statusParts = append(statusParts, m.buildModelStatusText())

	// Streaming state and feedback
	switch {
	case m.isStreaming:
		statusParts = append(
			statusParts,
			m.spinner.View()+" "+m.styles.status.Render("Thinking..."),
		)
	case m.showCopyFeedback:
		statusParts = append(statusParts, m.styles.status.Render("Copied"+checkmarkSuffix))
	case m.showModelUnavailableFeedback:
		warnStyle := lipgloss.NewStyle().Foreground(lipgloss.ANSIColor(ansiYellow))

		feedback := "Models unavailable"
		if m.modelUnavailableReason != "" {
			feedback += ": " + m.modelUnavailableReason
		}

		statusParts = append(statusParts, warnStyle.Render(feedback))
	case m.justCompleted:
		statusParts = append(statusParts, m.styles.status.Render("Ready"+checkmarkSuffix))
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

	return m.styles.input.Width(max(m.width-modalPadding, 1)).Render(m.textarea.View())
}

// renderFooter renders the context-aware help text footer using bubbles/help.
func (m *Model) renderFooter() string {
	return lipgloss.NewStyle().MaxWidth(m.width).Inline(true).Render(m.renderShortHelp())
}

// buildModelStatusText renders the model indicator for the status bar.
// Shows "auto → resolved-model" when in auto mode with a resolved model,
// plain "auto" when not yet resolved, or the explicit model ID otherwise.
func (m *Model) buildModelStatusText() string {
	modelStyle := lipgloss.NewStyle().Foreground(m.theme.DimColor)

	switch {
	case m.isAutoMode():
		if resolved := m.resolvedAutoModel(); resolved != "" {
			return modelStyle.Render(modelAuto + " \u2192 " + resolved)
		}

		return modelStyle.Render(modelAuto)
	case m.currentModel != "":
		return modelStyle.Render(m.currentModel)
	default:
		return modelStyle.Render(modelAuto)
	}
}
