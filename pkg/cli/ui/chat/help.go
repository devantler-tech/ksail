package chat

import (
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/lipgloss"
)

// Help key symbols for consistent rendering.
const (
	keyEnter   = "⏎"
	keyEscape  = "esc"
	keyArrows  = "↑↓"
	keyPageNav = "PgUp/Dn"
	helpSep    = " • "
)

// helpKeyStyle renders keybinding keys.
var helpKeyStyle = lipgloss.NewStyle().
	Foreground(toolColor)

// helpDescStyle renders keybinding descriptions.
var helpDescStyle = lipgloss.NewStyle().
	Foreground(dimColor) // Muted gray for subtle descriptions

// createHelpModel creates a configured help model.
func createHelpModel() help.Model {
	h := help.New()
	h.ShortSeparator = " • "
	h.FullSeparator = "   "
	h.Ellipsis = "…"
	h.Styles = help.Styles{
		ShortKey:       helpKeyStyle,
		ShortDesc:      helpDescStyle,
		ShortSeparator: helpStyle,
		Ellipsis:       helpStyle,
		FullKey:        helpKeyStyle,
		FullDesc:       helpDescStyle,
		FullSeparator:  helpStyle,
	}
	return h
}

// renderHelpOverlay renders the full help overlay modal matching the input area size.
func (m *Model) renderHelpOverlay() string {
	modalWidth := m.width - 2
	contentWidth := max(modalWidth-4, 1)
	clipStyle := lipgloss.NewStyle().MaxWidth(contentWidth).Inline(true)

	// Compact help content fitting in input area height (3 lines of content)
	var content strings.Builder
	content.WriteString(clipStyle.Render(
		helpKeyStyle.Render(keyEnter)+" send"+helpSep+
			helpKeyStyle.Render("Alt+"+keyEnter)+" newline"+helpSep+
			helpKeyStyle.Render(keyArrows)+" history"+helpSep+
			helpKeyStyle.Render(keyPageNav)+" scroll") + "\n")
	content.WriteString(clipStyle.Render(
		helpKeyStyle.Render("Tab")+" mode"+helpSep+
			helpKeyStyle.Render("^T")+" tools"+helpSep+
			helpKeyStyle.Render("^Y")+" copy"+helpSep+
			helpKeyStyle.Render("^H")+" sessions"+helpSep+
			helpKeyStyle.Render("^O")+" model"+helpSep+
			helpKeyStyle.Render("^N")+" new") + "\n")
	content.WriteString(clipStyle.Render(
		helpKeyStyle.Render(keyEscape) + " close" + helpSep +
			helpKeyStyle.Render("^C") + " quit"))

	// Use same height as input area (inputHeight = 3)
	contentStr := content.String()
	modalStyle := createPickerModalStyle(modalWidth, inputHeight)
	return modalStyle.Render(contentStr)
}

// renderShortHelp renders the context-aware footer help text using the KeyMap.
// It intelligently truncates to fit available width while always showing "F1 help".
func (m *Model) renderShortHelp() string {
	availWidth := max(m.width-4, 20) // Account for padding
	helpToggle := helpKeyStyle.Render("F1") + " help"
	helpToggleWidth := lipgloss.Width(helpToggle)
	usableWidth := availWidth - helpToggleWidth - 3 // Space for separator

	parts := m.getContextHelpParts()
	result := buildTruncatedHelp(parts, usableWidth)

	// Always append help toggle
	if result != "" {
		result += helpSep
	}
	result += helpToggle

	return helpStyle.Render("  " + result)
}

// getContextHelpParts returns help parts based on current UI context.
func (m *Model) getContextHelpParts() []string {
	switch {
	case m.showHelpOverlay:
		return []string{helpKeyStyle.Render(keyEscape) + " close"}

	case m.pendingPermission != nil:
		return getPermissionHelpParts()

	case m.showModelPicker:
		return m.getModelPickerHelpParts()

	case m.showSessionPicker:
		return m.getSessionPickerHelpParts()

	default:
		return m.getDefaultHelpParts()
	}
}

// getPermissionHelpParts returns help for permission prompts.
func getPermissionHelpParts() []string {
	return []string{
		helpKeyStyle.Render("y") + " allow",
		helpKeyStyle.Render("n") + " deny",
		helpKeyStyle.Render("a") + " always",
	}
}

// getModelPickerHelpParts returns help for model picker state.
func (m *Model) getModelPickerHelpParts() []string {
	if m.modelFilterActive {
		return getFilterModeHelpParts()
	}
	return []string{
		helpKeyStyle.Render(keyArrows) + " select",
		helpKeyStyle.Render("/") + " filter",
		helpKeyStyle.Render(keyEnter) + " confirm",
		helpKeyStyle.Render(keyEscape) + " cancel",
	}
}

// getSessionPickerHelpParts returns help for session picker state.
func (m *Model) getSessionPickerHelpParts() []string {
	switch {
	case m.renamingSession:
		return []string{
			helpKeyStyle.Render(keyEnter) + " save",
			helpKeyStyle.Render(keyEscape) + " cancel",
		}
	case m.confirmDeleteSession:
		return []string{
			helpKeyStyle.Render("y") + " delete",
			helpKeyStyle.Render("n") + " cancel",
		}
	case m.sessionFilterActive:
		return getFilterModeHelpParts()
	default:
		return []string{
			helpKeyStyle.Render(keyArrows) + " select",
			helpKeyStyle.Render("/") + " filter",
			helpKeyStyle.Render("r") + " rename",
			helpKeyStyle.Render("d") + " delete",
			helpKeyStyle.Render(keyEscape) + " close",
		}
	}
}

// getDefaultHelpParts returns help for the default chat view.
func (m *Model) getDefaultHelpParts() []string {
	modeIcon := "</>"
	if !m.agentMode {
		modeIcon = "≡"
	}

	parts := []string{
		helpKeyStyle.Render(keyEnter) + " send",
	}

	// Conditionally add copy hint
	if m.hasAssistantMessages() {
		parts = append(parts, helpKeyStyle.Render("^Y")+" copy")
	}

	parts = append(parts, helpKeyStyle.Render("Tab")+" "+modeIcon)

	// Conditionally add tools hint
	if len(m.toolOrder) > 0 || m.hasToolsInMessages() {
		parts = append(parts, helpKeyStyle.Render("^T")+" tools")
	}

	parts = append(parts,
		helpKeyStyle.Render("^H")+" sessions",
		helpKeyStyle.Render("^O")+" model",
		helpKeyStyle.Render("^N")+" new",
	)

	return parts
}

// buildTruncatedHelp builds a help string that fits within maxWidth.
func buildTruncatedHelp(parts []string, maxWidth int) string {
	var result strings.Builder
	currentWidth := 0

	for i, part := range parts {
		partWidth := lipgloss.Width(part)
		sepWidth := 0
		if i > 0 {
			sepWidth = lipgloss.Width(helpSep)
		}

		if currentWidth+sepWidth+partWidth > maxWidth {
			break
		}

		if i > 0 {
			result.WriteString(helpSep)
		}
		result.WriteString(part)
		currentWidth += sepWidth + partWidth
	}

	return result.String()
}

// getFilterModeHelpParts returns help parts for filter mode (used by both model and session pickers).
func getFilterModeHelpParts() []string {
	return []string{
		helpKeyStyle.Render(keyEnter) + " done",
		helpKeyStyle.Render(keyEscape) + " clear",
	}
}

// hasToolsInMessages checks if any messages have tools.
func (m *Model) hasToolsInMessages() bool {
	for _, msg := range m.messages {
		if msg.role == "assistant" && len(msg.tools) > 0 {
			return true
		}
	}
	return false
}

// hasAssistantMessages checks if there are any completed assistant messages.
func (m *Model) hasAssistantMessages() bool {
	for _, msg := range m.messages {
		if msg.role == "assistant" && msg.content != "" && !msg.isStreaming {
			return true
		}
	}
	return false
}
