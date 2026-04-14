package chat

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	copilot "github.com/github/copilot-sdk/go"
)

// pickerBorderLines is the number of border lines added by the popup style (top + bottom).
const pickerBorderLines = 2

// updateCommandPicker checks the textarea content and shows/hides the
// slash-command autocomplete popup or the option picker. Called after every textarea update.
func (m *Model) updateCommandPicker() {
	text := m.textarea.Value()

	// Must start with "/"
	if !strings.HasPrefix(text, "/") {
		m.dismissAllPickers()

		return
	}

	// Check if we have a space — if so, we may be in option-picking mode
	if strings.Contains(text, " ") {
		m.showCommandPicker = false
		m.commandPickerIndex = 0
		m.filteredCommands = nil
		m.updateOptionPicker(text)

		return
	}

	// No space — we're in command-picking mode
	m.showOptionPicker = false
	m.optionPickerIndex = 0
	m.filteredOptions = nil
	m.activeCommandName = ""

	partial := strings.ToLower(text[1:]) // strip leading "/"

	commands := m.getRegisteredCommands()
	if len(commands) == 0 {
		m.showCommandPicker = false

		return
	}

	var filtered []copilot.CommandDefinition

	for _, cmd := range commands {
		if strings.HasPrefix(strings.ToLower(cmd.Name), partial) {
			filtered = append(filtered, cmd)
		}
	}

	if len(filtered) == 0 {
		m.showCommandPicker = false
		m.commandPickerIndex = 0
		m.filteredCommands = nil

		return
	}

	m.filteredCommands = filtered
	m.showCommandPicker = true

	// Clamp index to valid range
	if m.commandPickerIndex >= len(filtered) {
		m.commandPickerIndex = len(filtered) - 1
	}
}

// updateOptionPicker handles the option picker when the user has typed a command name + space.
func (m *Model) updateOptionPicker(text string) {
	// Parse: "/mode plan" → cmdName="mode", argText="plan"
	withoutSlash := text[1:]
	parts := strings.SplitN(withoutSlash, " ", 2) //nolint:mnd // split into command + args
	cmdName := strings.ToLower(parts[0])

	argText := ""
	if len(parts) > 1 {
		argText = strings.ToLower(strings.TrimLeft(parts[1], " \t"))
	}

	// Look up option provider for this command
	provider, ok := m.commandOptions[cmdName]
	if !ok {
		m.showOptionPicker = false
		m.optionPickerIndex = 0
		m.filteredOptions = nil
		m.activeCommandName = ""

		return
	}

	allOptions := provider(m)
	if len(allOptions) == 0 {
		m.showOptionPicker = false

		return
	}

	// Filter options by prefix match on the argument text
	var filtered []CommandOption

	for _, opt := range allOptions {
		if strings.HasPrefix(strings.ToLower(opt.Name), argText) {
			filtered = append(filtered, opt)
		}
	}

	if len(filtered) == 0 {
		m.showOptionPicker = false
		m.optionPickerIndex = 0
		m.filteredOptions = nil
		m.activeCommandName = ""

		return
	}

	m.activeCommandName = cmdName
	m.filteredOptions = filtered
	m.showOptionPicker = true

	// Clamp index
	if m.optionPickerIndex >= len(filtered) {
		m.optionPickerIndex = len(filtered) - 1
	}
}

// dismissAllPickers hides both command and option pickers and resets their state.
func (m *Model) dismissAllPickers() {
	m.showCommandPicker = false
	m.commandPickerIndex = 0
	m.filteredCommands = nil
	m.showOptionPicker = false
	m.optionPickerIndex = 0
	m.filteredOptions = nil
	m.activeCommandName = ""
}

// commandHasOptions returns true if the currently highlighted command has registered options.
func (m *Model) commandHasOptions() bool {
	if m.commandPickerIndex >= len(m.filteredCommands) {
		return false
	}

	cmdName := strings.ToLower(m.filteredCommands[m.commandPickerIndex].Name)
	_, ok := m.commandOptions[cmdName]

	return ok
}

// getRegisteredCommands returns the slash commands from the session config.
func (m *Model) getRegisteredCommands() []copilot.CommandDefinition {
	if m.sessionConfig == nil {
		return nil
	}

	return m.sessionConfig.Commands
}

// handleCommandPickerKey handles keyboard input when the command picker popup is active.
func (m *Model) handleCommandPickerKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.commandPickerIndex > 0 {
			m.commandPickerIndex--
		}

		return m, nil
	case keyDown, "j":
		if m.commandPickerIndex < len(m.filteredCommands)-1 {
			m.commandPickerIndex++
		}

		return m, nil
	case keyEnter:
		// If the command has options, select without firing (like Tab)
		// so the user can pick an option. Otherwise fire immediately.
		if m.commandHasOptions() {
			return m.selectCommandWithoutFiring()
		}

		return m.selectAndFireCommand()
	case keyTab:
		// Fill the command text but don't submit
		return m.selectCommandWithoutFiring()
	case keyEscape:
		m.dismissAllPickers()

		return m, nil
	case keyCtrlC:
		return m.handleQuit()
	}

	// For any other key (typing more characters), update textarea then re-filter
	var taCmd tea.Cmd
	m.textarea, taCmd = m.textarea.Update(msg)
	m.updateCommandPicker()

	return m, taCmd
}

// handleOptionPickerKey handles keyboard input when the option picker popup is active.
func (m *Model) handleOptionPickerKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.optionPickerIndex > 0 {
			m.optionPickerIndex--
		}

		return m, nil
	case keyDown, "j":
		if m.optionPickerIndex < len(m.filteredOptions)-1 {
			m.optionPickerIndex++
		}

		return m, nil
	case keyEnter:
		return m.selectAndFireOption()
	case keyTab:
		return m.selectOptionWithoutFiring()
	case keyEscape:
		m.dismissAllPickers()

		return m, nil
	case keyCtrlC:
		return m.handleQuit()
	}

	// For any other key, update textarea then re-filter
	var taCmd tea.Cmd
	m.textarea, taCmd = m.textarea.Update(msg)
	m.updateCommandPicker()

	return m, taCmd
}

// selectedCommandValue returns the text for the selected command, or empty if out of bounds.
func (m *Model) selectedCommandValue() string {
	if m.commandPickerIndex >= len(m.filteredCommands) {
		return ""
	}

	return "/" + m.filteredCommands[m.commandPickerIndex].Name
}

// selectedOptionValue returns the text for the selected option, or empty if out of bounds.
func (m *Model) selectedOptionValue() string {
	if m.optionPickerIndex >= len(m.filteredOptions) {
		return ""
	}

	return "/" + m.activeCommandName + " " + m.filteredOptions[m.optionPickerIndex].Name
}

// selectAndFireCommand fills the selected command into the textarea and submits it.
func (m *Model) selectAndFireCommand() (tea.Model, tea.Cmd) {
	value := m.selectedCommandValue()
	if value == "" {
		return m, nil
	}

	m.textarea.SetValue(value)
	m.dismissAllPickers()

	return m.handleEnter()
}

// selectCommandWithoutFiring fills the selected command + a trailing space, without submitting.
func (m *Model) selectCommandWithoutFiring() (tea.Model, tea.Cmd) {
	value := m.selectedCommandValue()
	if value == "" {
		return m, nil
	}

	m.textarea.SetValue(value + " ")
	m.showCommandPicker = false
	m.commandPickerIndex = 0
	m.filteredCommands = nil

	// Trigger option picker for the newly filled command
	m.updateCommandPicker()

	return m, nil
}

// selectAndFireOption fills the selected option and fires the command.
func (m *Model) selectAndFireOption() (tea.Model, tea.Cmd) {
	value := m.selectedOptionValue()
	if value == "" {
		return m, nil
	}

	m.textarea.SetValue(value)
	m.dismissAllPickers()

	return m.handleEnter()
}

// selectOptionWithoutFiring fills the selected option without submitting.
func (m *Model) selectOptionWithoutFiring() (tea.Model, tea.Cmd) {
	value := m.selectedOptionValue()
	if value == "" {
		return m, nil
	}

	m.textarea.SetValue(value + " ")
	m.dismissAllPickers()

	return m, nil
}

// pickerItem represents a single item in a picker popup.
type pickerItem struct {
	label       string
	description string
}

// isModalActive returns true if any modal overlay is currently visible.
func (m *Model) isModalActive() bool {
	return m.pendingPermission != nil ||
		m.pendingElicitation != nil ||
		m.showModelPicker ||
		m.showSessionPicker ||
		m.showReasoningPicker ||
		m.showHelpOverlay
}

// renderPickerPopup renders the floating autocomplete popup for either
// commands or options, depending on which picker is active.
// Returns empty string when any modal is active (modals take priority).
func (m *Model) renderPickerPopup() string {
	// Suppress picker when a modal is active — modals take visual priority
	if m.isModalActive() {
		return ""
	}

	if m.showOptionPicker && len(m.filteredOptions) > 0 {
		items := make([]pickerItem, len(m.filteredOptions))
		for i, opt := range m.filteredOptions {
			items[i] = pickerItem{label: opt.Name, description: opt.Description}
		}

		return m.renderPickerItems(items, m.optionPickerIndex)
	}

	if m.showCommandPicker && len(m.filteredCommands) > 0 {
		items := make([]pickerItem, len(m.filteredCommands))
		for i, cmd := range m.filteredCommands {
			items[i] = pickerItem{label: "/" + cmd.Name, description: cmd.Description}
		}

		return m.renderPickerItems(items, m.commandPickerIndex)
	}

	return ""
}

// renderPickerItems renders a generic picker popup with highlighted selection.
func (m *Model) renderPickerItems(items []pickerItem, selectedIndex int) string {
	if len(items) == 0 {
		return ""
	}

	modalWidth := max(m.width-modalPadding, 1)
	contentWidth := max(modalWidth-contentPadding, 1)
	clipStyle := lipgloss.NewStyle().MaxWidth(contentWidth).Inline(true)

	highlightStyle := lipgloss.NewStyle().
		Foreground(lipgloss.ANSIColor(ansiBlack)).
		Background(lipgloss.ANSIColor(ansiCyan))

	dimStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240"))

	var content strings.Builder

	for i, item := range items {
		if i == selectedIndex {
			styledLine := highlightStyle.Render(item.label)
			if item.description != "" {
				styledLine += " " + item.description
			}

			content.WriteString(clipStyle.Render(styledLine) + "\n")
		} else {
			styledLine := item.label
			if item.description != "" {
				styledLine += " " + dimStyle.Render(item.description)
			}

			content.WriteString(clipStyle.Render(styledLine) + "\n")
		}
	}

	popupStyle := lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(m.theme.PrimaryColor).
		PaddingLeft(1).
		PaddingRight(1).
		Width(modalWidth)

	return popupStyle.Render(strings.TrimRight(content.String(), "\n"))
}

// pickerExtraHeight returns the extra height consumed by command or option picker popups.
func (m *Model) pickerExtraHeight() int {
	if m.showOptionPicker && len(m.filteredOptions) > 0 {
		return len(m.filteredOptions) + pickerBorderLines
	}

	if m.showCommandPicker && len(m.filteredCommands) > 0 {
		return len(m.filteredCommands) + pickerBorderLines
	}

	return 0
}

// commandPickerExtraHeight returns the extra height consumed by the command picker popup.
//
// Deprecated: Use pickerExtraHeight instead, which handles both pickers.
func (m *Model) commandPickerExtraHeight() int {
	return m.pickerExtraHeight()
}

// overlayBottom composites the popup string over the bottom lines of the base string.
// The popup replaces the last N lines of base, where N is the popup's line count.
func overlayBottom(base, popup string) string {
	baseLines := strings.Split(base, "\n")
	popupLines := strings.Split(popup, "\n")

	startIdx := len(baseLines) - len(popupLines)
	if startIdx < 0 {
		startIdx = 0
	}

	for i, pLine := range popupLines {
		idx := startIdx + i
		if idx < len(baseLines) {
			baseLines[idx] = pLine
		}
	}

	return strings.Join(baseLines, "\n")
}
