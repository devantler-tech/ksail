package chat

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	copilot "github.com/github/copilot-sdk/go"
)

// FilterEnabledModels returns only models with an enabled policy state.
// This is used by the lazy-load model picker and can be called from outside the package.
func FilterEnabledModels(allModels []copilot.ModelInfo) []copilot.ModelInfo {
	var models []copilot.ModelInfo

	for _, m := range allModels {
		if m.Policy != nil && m.Policy.State == "enabled" {
			models = append(models, m)
		}
	}

	return models
}

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
		// "auto" option selected — switch only if not already in auto mode
		if !m.isAutoMode() {
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

	session, err := m.client.CreateSession(m.ctx, m.sessionConfig)
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
	return m.renderPickerModalContent(
		len(m.filteredModels)+1,
		m.modelPickerIndex,
		m.renderModelPickerTitle,
		m.renderModelItems,
	)
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

// isAutoMode returns true when the user has selected "auto" model (not an explicit model).
func (m *Model) isAutoMode() bool {
	return m.sessionConfig.Model == "" || m.sessionConfig.Model == modelAuto
}

// resolvedAutoModel returns the server-resolved model ID when in auto mode,
// or empty string if auto hasn't resolved yet.
func (m *Model) resolvedAutoModel() string {
	if !m.isAutoMode() {
		return ""
	}

	if m.currentModel != "" && m.currentModel != modelAuto {
		return m.currentModel
	}

	return ""
}

// findModelMultiplier looks up the billing multiplier for a model ID.
// Returns 0 if the model is not found or has no billing info.
func (m *Model) findModelMultiplier(modelID string) float64 {
	for _, model := range m.availableModels {
		if model.ID == modelID && model.Billing != nil {
			return model.Billing.Multiplier
		}
	}

	return 0
}

// formatModelItem formats a single model item for display.
func (m *Model) formatModelItem(index int) (string, bool) {
	prefix := "  "
	if index == m.modelPickerIndex {
		prefix = "> "
	}

	if index == 0 {
		line := prefix + m.formatAutoOption()

		isCurrentModel := m.isAutoMode()
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

	// Check explicit model match (not auto-resolved)
	isCurrentModel := !m.isAutoMode() && model.ID == m.currentModel
	if isCurrentModel {
		line += checkmarkSuffix
	}

	return line, isCurrentModel
}

// formatAutoOption formats the "auto" picker item, showing the resolved model
// and its billing multiplier when available.
func (m *Model) formatAutoOption() string {
	resolved := m.resolvedAutoModel()
	if resolved == "" {
		return "auto (let Copilot choose)"
	}

	mult := m.findModelMultiplier(resolved)
	if mult > 0 {
		return fmt.Sprintf("auto (%s \u00b7 %.0fx)", resolved, mult)
	}

	return fmt.Sprintf("auto (%s)", resolved)
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
