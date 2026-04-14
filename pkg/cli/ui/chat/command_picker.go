package chat

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	copilot "github.com/github/copilot-sdk/go"
)

// updateCommandPicker checks the textarea content and shows/hides the
// slash-command autocomplete popup. Called after every textarea update.
func (m *Model) updateCommandPicker() {
	text := m.textarea.Value()

	// Only show when input starts with "/" and has no spaces yet (still typing command name)
	if !strings.HasPrefix(text, "/") || strings.Contains(text, " ") {
		m.showCommandPicker = false
		m.commandPickerIndex = 0
		m.filteredCommands = nil

		return
	}

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
		// Fill the command and submit
		return m.selectAndFireCommand()
	case "tab":
		// Fill the command text but don't submit
		return m.selectCommandWithoutFiring()
	case keyEscape:
		m.showCommandPicker = false
		m.commandPickerIndex = 0
		m.filteredCommands = nil

		return m, nil
	case keyCtrlC:
		return m.handleQuit(true)
	}

	// For any other key (typing more characters), update textarea then re-filter
	var taCmd tea.Cmd
	m.textarea, taCmd = m.textarea.Update(msg)
	m.updateCommandPicker()

	return m, taCmd
}

// selectAndFireCommand fills the selected command into the textarea and submits it.
func (m *Model) selectAndFireCommand() (tea.Model, tea.Cmd) {
	if m.commandPickerIndex >= len(m.filteredCommands) {
		return m, nil
	}

	cmd := m.filteredCommands[m.commandPickerIndex]
	m.textarea.SetValue("/" + cmd.Name)

	m.showCommandPicker = false
	m.commandPickerIndex = 0
	m.filteredCommands = nil

	// Trigger submit — reuse the existing handleEnter flow
	return m.handleEnter()
}

// selectCommandWithoutFiring fills the selected command + a trailing space, without submitting.
func (m *Model) selectCommandWithoutFiring() (tea.Model, tea.Cmd) {
	if m.commandPickerIndex >= len(m.filteredCommands) {
		return m, nil
	}

	cmd := m.filteredCommands[m.commandPickerIndex]
	m.textarea.SetValue("/" + cmd.Name + " ")

	m.showCommandPicker = false
	m.commandPickerIndex = 0
	m.filteredCommands = nil

	return m, nil
}

// renderCommandPickerPopup renders the floating command autocomplete popup.
func (m *Model) renderCommandPickerPopup() string {
	if !m.showCommandPicker || len(m.filteredCommands) == 0 {
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

	for i, cmd := range m.filteredCommands {
		line := fmt.Sprintf("/%s", cmd.Name)
		desc := cmd.Description

		if i == m.commandPickerIndex {
			// Highlight the selected item
			styledLine := highlightStyle.Render(line)
			if desc != "" {
				styledLine += " " + desc
			}

			content.WriteString(clipStyle.Render(styledLine) + "\n")
		} else {
			styledLine := line
			if desc != "" {
				styledLine += " " + dimStyle.Render(desc)
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

// commandPickerExtraHeight returns the extra height consumed by the command picker popup.
func (m *Model) commandPickerExtraHeight() int {
	if !m.showCommandPicker || len(m.filteredCommands) == 0 {
		return 0
	}

	// Each command is 1 line + 2 for border (top + bottom)
	return len(m.filteredCommands) + 2
}
