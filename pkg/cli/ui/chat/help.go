package chat

import (
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/lipgloss"
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
		helpKeyStyle.Render("⏎")+" send • "+
			helpKeyStyle.Render("Alt+⏎")+" newline • "+
			helpKeyStyle.Render("↑↓")+" history • "+
			helpKeyStyle.Render("PgUp/Dn")+" scroll") + "\n")
	content.WriteString(clipStyle.Render(
		helpKeyStyle.Render("Tab")+" mode • "+
			helpKeyStyle.Render("^T")+" tools • "+
			helpKeyStyle.Render("^H")+" sessions • "+
			helpKeyStyle.Render("^O")+" model • "+
			helpKeyStyle.Render("^N")+" new") + "\n")
	content.WriteString(clipStyle.Render(
		helpKeyStyle.Render("esc") + " close • " +
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

	var parts []string

	// Context-specific help hints (keywords only)
	switch {
	case m.showHelpOverlay:
		parts = []string{helpKeyStyle.Render("esc") + " close"}

	case m.pendingPermission != nil:
		parts = []string{
			helpKeyStyle.Render("y") + " allow",
			helpKeyStyle.Render("n") + " deny",
			helpKeyStyle.Render("a") + " always",
		}

	case m.showModelPicker:
		if m.modelFilterActive {
			parts = getFilterModeHelpParts()
		} else {
			parts = []string{
				helpKeyStyle.Render("↑↓") + " select",
				helpKeyStyle.Render("/") + " filter",
				helpKeyStyle.Render("⏎") + " confirm",
				helpKeyStyle.Render("esc") + " cancel",
			}
		}

	case m.showSessionPicker:
		if m.renamingSession {
			parts = []string{
				helpKeyStyle.Render("⏎") + " save",
				helpKeyStyle.Render("esc") + " cancel",
			}
		} else if m.confirmDeleteSession {
			parts = []string{
				helpKeyStyle.Render("y") + " delete",
				helpKeyStyle.Render("n") + " cancel",
			}
		} else if m.sessionFilterActive {
			parts = getFilterModeHelpParts()
		} else {
			parts = []string{
				helpKeyStyle.Render("↑↓") + " select",
				helpKeyStyle.Render("/") + " filter",
				helpKeyStyle.Render("r") + " rename",
				helpKeyStyle.Render("d") + " delete",
				helpKeyStyle.Render("esc") + " close",
			}
		}

	default:
		// Default view - mode icon + common shortcuts
		modeIcon := "</>"
		if !m.agentMode {
			modeIcon = "≡"
		}
		parts = []string{
			helpKeyStyle.Render("⏎") + " send",
			helpKeyStyle.Render("Tab") + " " + modeIcon,
			helpKeyStyle.Render("^H") + " sessions",
			helpKeyStyle.Render("^O") + " model",
			helpKeyStyle.Render("^N") + " new",
		}
		// Add tool expand hint if tools exist
		if len(m.toolOrder) > 0 || m.hasToolsInMessages() {
			parts = append(
				parts[:2],
				append([]string{helpKeyStyle.Render("^T") + " tools"}, parts[2:]...)...)
		}
	}

	// Build help string, fitting as many parts as possible within usableWidth
	var result strings.Builder
	sep := " • "
	currentWidth := 0

	for i, part := range parts {
		partWidth := lipgloss.Width(part)
		sepWidth := 0
		if i > 0 {
			sepWidth = lipgloss.Width(sep)
		}

		if currentWidth+sepWidth+partWidth > usableWidth {
			break // Can't fit more
		}

		if i > 0 {
			result.WriteString(sep)
		}
		result.WriteString(part)
		currentWidth += sepWidth + partWidth
	}

	// Always append help toggle
	if result.Len() > 0 {
		result.WriteString(" • ")
	}
	result.WriteString(helpToggle)

	return helpStyle.Render("  " + result.String())
}

// getFilterModeHelpParts returns help parts for filter mode (used by both model and session pickers).
func getFilterModeHelpParts() []string {
	return []string{
		helpKeyStyle.Render("⏎") + " done",
		helpKeyStyle.Render("esc") + " clear",
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
