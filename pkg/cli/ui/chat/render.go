package chat

import (
	"fmt"
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

	var headerContent strings.Builder
	headerContent.WriteString(logoRendered)
	headerContent.WriteString("\n")
	headerContent.WriteString(taglineRow)

	return m.styles.headerBox.Width(max(m.width-modalPadding, 1)).Render(headerContent.String())
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

	// Reasoning effort — show level when set
	if effortText := m.buildReasoningEffortStatusText(); effortText != "" {
		statusParts = append(statusParts, effortText)
	}

	// Streaming state and feedback
	switch {
	case m.isStreaming:
		statusParts = append(statusParts, m.spinner.View())
	case m.showCopyFeedback:
		statusParts = append(statusParts, m.styles.status.Render("Copied"+checkmarkSuffix))
	case m.showModelUnavailableFeedback:
		warnStyle := lipgloss.NewStyle().Foreground(lipgloss.ANSIColor(ansiYellow))

		var feedback strings.Builder
		feedback.WriteString("Models unavailable")
		if m.modelUnavailableReason != "" {
			feedback.WriteString(": ")
			feedback.WriteString(m.modelUnavailableReason)
		}

		statusParts = append(statusParts, warnStyle.Render(feedback.String()))
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

	if m.showReasoningPicker {
		return m.renderReasoningPickerModal()
	}

	if m.showSessionPicker {
		return m.renderSessionPickerModal()
	}

	return m.styles.input.Width(max(m.width-modalPadding, 1)).Render(m.textarea.View())
}

// renderFooter renders the help text (left) and quota snapshot (right) on a single line.
// Help text is always prioritised — quota is placed in remaining space and clipped if needed.
func (m *Model) renderFooter() string {
	helpText := m.renderShortHelp()
	quotaText := m.buildQuotaStatusText()

	if quotaText == "" {
		return lipgloss.NewStyle().MaxWidth(m.width).Inline(true).Render(helpText)
	}

	helpWidth := lipgloss.Width(helpText)
	quotaWidth := lipgloss.Width(quotaText)
	spacing := m.width - helpWidth - quotaWidth

	if spacing < minSpacing {
		// Not enough room for quota — show help only.
		return lipgloss.NewStyle().MaxWidth(m.width).Inline(true).Render(helpText)
	}

	row := helpText + strings.Repeat(" ", spacing) + quotaText

	return lipgloss.NewStyle().MaxWidth(m.width).Inline(true).Render(row)
}

// buildModelStatusText renders the model indicator for the status bar.
// Shows "auto → resolved-model (0.9x)" when in auto mode with a resolved model,
// "auto (-10%)" when not yet resolved, or the explicit model ID otherwise.
func (m *Model) buildModelStatusText() string {
	modelStyle := lipgloss.NewStyle().Foreground(m.theme.DimColor)

	switch {
	case m.isAutoMode():
		resolved := m.resolvedAutoModel()
		if resolved == "" {
			return modelStyle.Render(modelAuto + " (-10%)")
		}

		mult := m.findModelMultiplier(resolved)
		if mult > 0 {
			discounted := mult * autoDiscountFactor

			return modelStyle.Render(
				fmt.Sprintf("%s \u2192 %s (%.1fx)", modelAuto, resolved, discounted),
			)
		}

		return modelStyle.Render(modelAuto + " \u2192 " + resolved)
	case m.currentModel != "":
		return modelStyle.Render(m.currentModel)
	default:
		return modelStyle.Render(modelAuto)
	}
}

// buildReasoningEffortStatusText renders the reasoning effort indicator for the status bar.
// Returns an empty string when no reasoning effort is set.
func (m *Model) buildReasoningEffortStatusText() string {
	if m.sessionConfig.ReasoningEffort == "" {
		return ""
	}

	effortStyle := lipgloss.NewStyle().Foreground(m.theme.DimColor)

	return effortStyle.Render(m.sessionConfig.ReasoningEffort + " effort")
}

// buildQuotaStatusText renders the premium request quota indicator for the status bar.
// Shows "used/total reqs · X% · resets Jan 2" when quota data is available,
// or nothing if no quota snapshots have been received yet.
// Only the "premium" quota category is displayed — other categories (e.g., unlimited
// "chat" quotas) are ignored to keep the status bar stable and relevant.
func (m *Model) buildQuotaStatusText() string {
	if len(m.lastQuotaSnapshots) == 0 {
		return ""
	}

	// Only show the "premium" quota category — it's the one relevant for billing.
	snapshot, found := m.lastQuotaSnapshots["premium"]
	if !found {
		return ""
	}

	quotaStyle := lipgloss.NewStyle().Foreground(m.theme.DimColor)

	if snapshot.isUnlimited {
		return quotaStyle.Render("\u221e reqs")
	}

	var parts []string

	parts = append(
		parts,
		fmt.Sprintf("%.0f/%.0f reqs", snapshot.usedRequests, snapshot.entitlementRequests),
	)
	parts = append(parts, fmt.Sprintf("%.0f%%", snapshot.remainingPercentage))

	if snapshot.resetDate != "" {
		parts = append(parts, "resets "+snapshot.resetDate)
	}

	return quotaStyle.Render(strings.Join(parts, " \u00b7 "))
}
