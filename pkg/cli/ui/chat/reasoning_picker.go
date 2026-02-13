package chat

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// reasoningEffortLevels lists the selectable reasoning effort options.
// The first entry ("off") clears the effort setting; the rest map directly
// to the SDK's ReasoningEffort field.
var reasoningEffortLevels = []string{"off", "low", "medium", "high"}

// handleReasoningPickerKey handles keyboard input when the reasoning effort picker is active.
func (m *Model) handleReasoningPickerKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	totalItems := len(reasoningEffortLevels)

	switch msg.String() {
	case keyEscape, "ctrl+e":
		m.showReasoningPicker = false
		m.updateDimensions()

		return m, nil
	case "up", "k":
		if m.reasoningPickerIndex > 0 {
			m.reasoningPickerIndex--
		}

		return m, nil
	case keyDown, "j":
		if m.reasoningPickerIndex < totalItems-1 {
			m.reasoningPickerIndex++
		}

		return m, nil
	case keyEnter:
		return m.selectReasoningEffort()
	case keyCtrlC:
		m.cleanup()
		m.quitting = true

		return m, tea.Quit
	}

	return m, nil
}

// selectReasoningEffort applies the chosen reasoning effort level.
// "off" clears the setting; other values update the session config and recreate the session.
func (m *Model) selectReasoningEffort() (tea.Model, tea.Cmd) {
	selected := reasoningEffortLevels[m.reasoningPickerIndex]

	newEffort := selected
	if selected == "off" {
		newEffort = ""
	}

	// Only recreate session if the value actually changed
	if newEffort != m.sessionConfig.ReasoningEffort {
		return m.switchReasoningEffort(newEffort)
	}

	m.showReasoningPicker = false
	m.updateDimensions()

	return m, nil
}

// switchReasoningEffort changes the reasoning effort by recreating the session.
func (m *Model) switchReasoningEffort(newEffort string) (tea.Model, tea.Cmd) {
	m.cleanup()

	if m.session != nil {
		_ = m.session.Destroy()
	}

	m.sessionConfig.ReasoningEffort = newEffort

	session, err := m.client.CreateSession(m.ctx, m.sessionConfig)
	if err != nil {
		m.err = fmt.Errorf("failed to switch reasoning effort: %w", err)
		m.showReasoningPicker = false
		m.updateDimensions()

		return m, nil
	}

	m.session = session

	m.resetStreamingState()
	m.showReasoningPicker = false
	m.updateDimensions()
	m.updateViewportContent()

	return m, nil
}

// renderReasoningPickerModal renders the reasoning effort picker as an inline modal.
func (m *Model) renderReasoningPickerModal() string {
	return m.renderPickerModalContent(
		len(reasoningEffortLevels),
		m.reasoningPickerIndex,
		m.renderReasoningPickerTitle,
		m.renderReasoningItems,
	)
}

// renderReasoningPickerTitle renders the title for the reasoning picker modal.
func (m *Model) renderReasoningPickerTitle(listContent *strings.Builder, clipStyle lipgloss.Style) {
	listContent.WriteString(clipStyle.Render("Reasoning Effort") + "\n")
}

// renderReasoningItems renders the visible reasoning effort items.
func (m *Model) renderReasoningItems(
	listContent *strings.Builder,
	clipStyle lipgloss.Style,
	scrollOffset, endIdx int,
) {
	for i := scrollOffset; i < endIdx; i++ {
		line, isCurrent := m.formatReasoningItem(i)
		styledLine := m.styleReasoningItem(line, i, isCurrent)
		listContent.WriteString(clipStyle.Render(styledLine) + "\n")
	}
}

// formatReasoningItem formats a single reasoning effort item for display.
func (m *Model) formatReasoningItem(index int) (string, bool) {
	prefix := "  "
	if index == m.reasoningPickerIndex {
		prefix = "> "
	}

	level := reasoningEffortLevels[index]
	isCurrent := m.isCurrentReasoningEffort(level)

	line := prefix + level
	if isCurrent {
		line += checkmarkSuffix
	}

	return line, isCurrent
}

// isCurrentReasoningEffort checks whether a level matches the active reasoning effort.
func (m *Model) isCurrentReasoningEffort(level string) bool {
	current := m.sessionConfig.ReasoningEffort
	if level == "off" {
		return current == ""
	}

	return current == level
}

// styleReasoningItem applies styling to a reasoning effort item.
func (m *Model) styleReasoningItem(line string, index int, isCurrent bool) string {
	if index == m.reasoningPickerIndex {
		return lipgloss.NewStyle().
			Foreground(lipgloss.ANSIColor(ansiCyan)).
			Bold(true).
			Render(line)
	}

	if isCurrent {
		return lipgloss.NewStyle().
			Foreground(lipgloss.ANSIColor(ansiGreen)).
			Render(line)
	}

	return line
}

// findCurrentReasoningIndex returns the picker index for the current reasoning effort.
func (m *Model) findCurrentReasoningIndex() int {
	current := m.sessionConfig.ReasoningEffort
	if current == "" {
		return 0 // "off"
	}

	for idx, level := range reasoningEffortLevels {
		if level == current {
			return idx
		}
	}

	return 0
}

// currentModelSupportsReasoning returns true if the active model supports reasoning effort.
// It checks the model list for the current (or auto-resolved) model's capabilities.
func (m *Model) currentModelSupportsReasoning() bool {
	modelID := m.currentModel
	if m.isAutoMode() {
		modelID = m.resolvedAutoModel()
	}

	if modelID == "" {
		// No resolved model yet — optimistically show the keybind since auto mode
		// may resolve to a reasoning-capable model.
		return m.sessionConfig.ReasoningEffort != ""
	}

	for _, model := range m.availableModels {
		if model.ID == modelID {
			return model.Capabilities.Supports.ReasoningEffort
		}
	}

	return false
}
