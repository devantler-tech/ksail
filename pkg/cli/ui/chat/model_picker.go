package chat

import (
	"context"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	copilot "github.com/github/copilot-sdk/go"
)

// handleModelPickerKey handles keyboard input when the model picker is active.
func (m *Model) handleModelPickerKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	totalItems := len(m.filteredModels) + 1 // auto + filtered models

	// Handle filter mode
	if m.modelFilterActive {
		return m.handleModelFilterKey(msg)
	}

	switch msg.String() {
	case keyEscape, "ctrl+o":
		m.showModelPicker = false
		m.modelFilterText = ""
		m.modelFilterActive = false
		m.updateDimensions()

		return m, nil
	case "/":
		m.modelFilterActive = true

		return m, nil
	case "up", "k":
		if m.modelPickerIndex > 0 {
			m.modelPickerIndex--
		}

		return m, nil
	case keyDown, "j":
		if m.modelPickerIndex < totalItems-1 {
			m.modelPickerIndex++
		}

		return m, nil
	case keyEnter:
		return m.selectModel(totalItems)
	case keyCtrlC:
		m.cleanup()
		m.quitting = true

		return m, tea.Quit
	}

	return m, nil
}

// handleModelFilterKey handles keyboard input when filtering models.
func (m *Model) handleModelFilterKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case keyEnter:
		m.modelFilterActive = false

		return m, nil
	case keyEscape:
		// Clear filter and exit filter mode
		m.modelFilterText = ""
		m.modelFilterActive = false
		m.applyModelFilter()

		return m, nil
	case keyBackspace:
		if len(m.modelFilterText) > 0 {
			m.modelFilterText = m.modelFilterText[:len(m.modelFilterText)-1]
			m.applyModelFilter()
		}

		return m, nil
	case keyCtrlC:
		m.cleanup()
		m.quitting = true

		return m, tea.Quit
	default:
		if msg.Type == tea.KeyRunes {
			m.modelFilterText += string(msg.Runes)
			m.applyModelFilter()
		}

		return m, nil
	}
}

// applyModelFilter filters the available models based on the current filter text.
func (m *Model) applyModelFilter() {
	if m.modelFilterText == "" {
		m.filteredModels = m.availableModels
	} else {
		filterLower := strings.ToLower(m.modelFilterText)

		m.filteredModels = make([]copilot.ModelInfo, 0)

		for _, model := range m.availableModels {
			if strings.Contains(strings.ToLower(model.ID), filterLower) {
				m.filteredModels = append(m.filteredModels, model)
			}
		}
	}
	// Reset picker index if it's out of bounds
	maxIndex := len(m.filteredModels)
	if m.modelPickerIndex > maxIndex {
		m.modelPickerIndex = maxIndex
	}
}

// selectModel handles model selection from the picker.
func (m *Model) selectModel(totalItems int) (tea.Model, tea.Cmd) {
	if m.modelPickerIndex == 0 {
		// "auto" option selected
		if m.currentModel != "" && m.currentModel != modelAuto {
			return m.switchModel("")
		}
	} else if m.modelPickerIndex > 0 && m.modelPickerIndex < totalItems {
		selectedModel := m.filteredModels[m.modelPickerIndex-1]
		if selectedModel.ID != m.currentModel {
			return m.switchModel(selectedModel.ID)
		}
	}

	m.showModelPicker = false
	m.modelFilterText = ""
	m.modelFilterActive = false
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

	session, err := m.client.CreateSession(context.Background(), m.sessionConfig)
	if err != nil {
		m.err = fmt.Errorf("failed to switch model: %w", err)
		m.showModelPicker = false
		m.updateDimensions()

		return m, nil
	}

	m.session = session

	m.resetStreamingState()
	m.showModelPicker = false
	m.modelFilterText = ""
	m.modelFilterActive = false
	m.updateDimensions()
	m.updateViewportContent()

	return m, nil
}

// renderModelPickerModal renders the model picker as an inline modal section.
func (m *Model) renderModelPickerModal() string {
	modalWidth := max(m.width-modalPadding, 1)
	contentWidth := max(modalWidth-contentPadding, 1)
	clipStyle := lipgloss.NewStyle().MaxWidth(contentWidth).Inline(true)

	totalItems := len(m.filteredModels) + 1
	maxVisible := m.calculateMaxPickerVisible()
	visibleCount := min(totalItems, maxVisible)

	scrollOffset := calculatePickerScrollOffset(m.modelPickerIndex, totalItems, maxVisible)

	var listContent strings.Builder
	m.renderModelPickerTitle(&listContent, clipStyle)

	isScrollable := totalItems > maxVisible
	renderScrollIndicatorTop(&listContent, clipStyle, isScrollable, scrollOffset)

	endIdx := min(scrollOffset+visibleCount, totalItems)
	m.renderModelItems(&listContent, clipStyle, scrollOffset, endIdx)

	renderScrollIndicatorBottom(&listContent, clipStyle, isScrollable, endIdx, totalItems)

	content := strings.TrimRight(listContent.String(), "\n")

	return renderPickerModal(content, modalWidth, visibleCount, isScrollable)
}

// renderModelPickerTitle renders the title or filter input.
func (m *Model) renderModelPickerTitle(listContent *strings.Builder, clipStyle lipgloss.Style) {
	// Show filter input if filtering is active or has text
	if m.modelFilterActive || m.modelFilterText != "" {
		filterStyle := lipgloss.NewStyle().Foreground(lipgloss.ANSIColor(ansiCyan))

		cursor := ""
		if m.modelFilterActive {
			cursor = "_"
		}

		filterLine := filterStyle.Render("🔍 " + m.modelFilterText + cursor)
		listContent.WriteString(clipStyle.Render(filterLine) + "\n")
	} else {
		listContent.WriteString(clipStyle.Render("Select Model") + "\n")
	}
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

		isCurrentModel := m.currentModel == "" || m.currentModel == modelAuto
		if isCurrentModel {
			line += checkmarkSuffix
		}

		return line, isCurrentModel
	}

	model := m.filteredModels[index-1]

	multiplier := ""
	if model.Billing != nil && model.Billing.Multiplier > 0 {
		multiplier = fmt.Sprintf(" (%.0fx)", model.Billing.Multiplier)
	}

	line := fmt.Sprintf("%s%s%s", prefix, model.ID, multiplier)

	isCurrentModel := model.ID == m.currentModel
	if isCurrentModel {
		line += checkmarkSuffix
	}

	return line, isCurrentModel
}

// styleModelItem applies styling to a model item.
func (m *Model) styleModelItem(line string, index int, isCurrentModel bool) string {
	if index == m.modelPickerIndex {
		return lipgloss.NewStyle().
			Foreground(lipgloss.ANSIColor(ansiCyan)).
			Bold(true).
			Render(line)
	}

	if isCurrentModel {
		return lipgloss.NewStyle().
			Foreground(lipgloss.ANSIColor(ansiGreen)).
			Render(line)
	}

	return line
}
