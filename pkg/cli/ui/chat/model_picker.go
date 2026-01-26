package chat

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// handleModelPickerKey handles keyboard input when the model picker is active.
func (m *Model) handleModelPickerKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	totalItems := len(m.availableModels) + 1 // auto + models

	switch msg.String() {
	case "esc", "ctrl+o":
		m.showModelPicker = false
		m.updateDimensions()
		return m, nil
	case "up", "k":
		if m.modelPickerIndex > 0 {
			m.modelPickerIndex--
		}
		return m, nil
	case "down", "j":
		if m.modelPickerIndex < totalItems-1 {
			m.modelPickerIndex++
		}
		return m, nil
	case "enter":
		return m.selectModel(totalItems)
	case "ctrl+c":
		m.cleanup()
		m.quitting = true
		return m, tea.Quit
	}
	return m, nil
}

// selectModel handles model selection from the picker.
func (m *Model) selectModel(totalItems int) (tea.Model, tea.Cmd) {
	if m.modelPickerIndex == 0 {
		// "auto" option selected
		if m.currentModel != "" && m.currentModel != "auto" {
			return m.switchModel("")
		}
	} else if m.modelPickerIndex > 0 && m.modelPickerIndex < totalItems {
		selectedModel := m.availableModels[m.modelPickerIndex-1]
		if selectedModel.ID != m.currentModel {
			return m.switchModel(selectedModel.ID)
		}
	}
	m.showModelPicker = false
	m.updateDimensions()
	return m, nil
}

// switchModel changes to a new model by recreating the session.
func (m *Model) switchModel(newModelID string) (tea.Model, tea.Cmd) {
	m.cleanup()
	if m.session != nil {
		_ = m.session.Destroy()
	}

	m.sessionConfig.Model = newModelID
	m.currentModel = newModelID

	session, err := m.client.CreateSession(m.sessionConfig)
	if err != nil {
		m.err = fmt.Errorf("failed to switch model: %w", err)
		m.showModelPicker = false
		m.updateDimensions()
		return m, nil
	}
	m.session = session

	m.resetStreamingState()
	m.showModelPicker = false
	m.updateDimensions()
	m.updateViewportContent()
	return m, nil
}

// renderModelPickerModal renders the model picker as an inline modal section.
func (m *Model) renderModelPickerModal() string {
	modalWidth := m.width - 2
	contentWidth := max(modalWidth-4, 1)
	clipStyle := lipgloss.NewStyle().MaxWidth(contentWidth).Inline(true)

	totalItems := len(m.availableModels) + 1
	visibleCount := min(totalItems, maxPickerVisible)

	scrollOffset := calculatePickerScrollOffset(m.modelPickerIndex, totalItems, maxPickerVisible)

	var listContent strings.Builder
	listContent.WriteString(clipStyle.Render("Select Model") + "\n")

	isScrollable := totalItems > maxPickerVisible
	renderScrollIndicatorTop(&listContent, clipStyle, isScrollable, scrollOffset)

	endIdx := min(scrollOffset+visibleCount, totalItems)
	m.renderModelItems(&listContent, clipStyle, scrollOffset, endIdx)

	renderScrollIndicatorBottom(&listContent, clipStyle, isScrollable, endIdx, totalItems)

	content := strings.TrimRight(listContent.String(), "\n")
	return renderPickerModal(content, modalWidth, visibleCount, isScrollable)
}

// renderModelItems renders the visible model list items.
func (m *Model) renderModelItems(
	listContent *strings.Builder,
	clipStyle lipgloss.Style,
	scrollOffset, endIdx int,
) {
	for i := scrollOffset; i < endIdx; i++ {
		line, isCurrentModel := m.formatModelItem(i)
		styledLine := m.styleModelItem(line, i, isCurrentModel)
		listContent.WriteString(clipStyle.Render(styledLine) + "\n")
	}
}

// formatModelItem formats a single model item for display.
func (m *Model) formatModelItem(index int) (string, bool) {
	prefix := "  "
	if index == m.modelPickerIndex {
		prefix = "> "
	}

	if index == 0 {
		line := prefix + "auto (let Copilot choose)"
		isCurrentModel := m.currentModel == "" || m.currentModel == "auto"
		if isCurrentModel {
			line += " ✓"
		}
		return line, isCurrentModel
	}

	model := m.availableModels[index-1]
	multiplier := ""
	if model.Billing != nil && model.Billing.Multiplier > 0 {
		multiplier = fmt.Sprintf(" (%.0fx)", model.Billing.Multiplier)
	}

	line := fmt.Sprintf("%s%s%s", prefix, model.ID, multiplier)
	isCurrentModel := model.ID == m.currentModel
	if isCurrentModel {
		line += " ✓"
	}
	return line, isCurrentModel
}

// styleModelItem applies styling to a model item.
func (m *Model) styleModelItem(line string, index int, isCurrentModel bool) string {
	if index == m.modelPickerIndex {
		return lipgloss.NewStyle().
			Foreground(lipgloss.ANSIColor(14)).
			Bold(true).
			Render(line)
	}
	if isCurrentModel {
		return lipgloss.NewStyle().
			Foreground(lipgloss.ANSIColor(10)).
			Render(line)
	}
	return line
}
