package chat

import (
	"github.com/charmbracelet/lipgloss"
	"github.com/devantler-tech/ksail/v5/pkg/cli/ui/asciiart"
)

// logoHeight is the number of lines in the block letter logo (must be const for headerHeight calculation).
const logoHeight = 6

// Logo functions that delegate to the shared asciiart package.
var (
	// logo returns the ASCII art block letter logo.
	logo = asciiart.Logo

	// tagline returns the standard tagline.
	tagline = asciiart.Tagline
)

var (
	// Color palette - uses standard ANSI colors (0-15) to respect user's terminal theme.
	primaryColor   = lipgloss.ANSIColor(14) // Bright cyan
	accentColor    = lipgloss.ANSIColor(6)  // Cyan
	secondaryColor = lipgloss.ANSIColor(8)  // Bright black (gray)
	userColor      = lipgloss.ANSIColor(12) // Bright blue
	assistantColor = lipgloss.ANSIColor(13) // Bright magenta
	toolColor      = lipgloss.ANSIColor(11) // Bright yellow
	successColor   = lipgloss.ANSIColor(10) // Bright green
	dimColor       = lipgloss.ANSIColor(8)  // Bright black (gray)

	// logoStyle renders the ASCII art logo.
	logoStyle = lipgloss.NewStyle().
			Foreground(primaryColor).
			Bold(true)

	// taglineStyle renders the tagline under the logo.
	taglineStyle = lipgloss.NewStyle().
			Foreground(accentColor).
			Italic(true)

	// headerBoxStyle wraps the entire header section.
	headerBoxStyle = lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(primaryColor).
			Padding(0, 2)

	// userMsgStyle is the style for user messages.
	userMsgStyle = lipgloss.NewStyle().
			Foreground(userColor).
			Bold(true)

	// assistantMsgStyle is the style for assistant message labels.
	assistantMsgStyle = lipgloss.NewStyle().
				Foreground(assistantColor).
				Bold(true)

	// toolMsgStyle is the style for tool call/result messages.
	toolMsgStyle = lipgloss.NewStyle().
			Foreground(toolColor)

	// toolOutputStyle is the style for tool output text.
	toolOutputStyle = lipgloss.NewStyle().
			Foreground(dimColor)

	// helpStyle is the style for help text.
	helpStyle = lipgloss.NewStyle().
			Foreground(dimColor)

	// spinnerStyle is the style for the loading spinner.
	spinnerStyle = lipgloss.NewStyle().
			Foreground(accentColor)

	// viewportStyle styles the chat area.
	viewportStyle = lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(secondaryColor).
			Padding(0, 1)

	// inputStyle styles the input textarea.
	inputStyle = lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(primaryColor).
			Padding(0, 1)

	// statusStyle is for status messages.
	statusStyle = lipgloss.NewStyle().
			Foreground(dimColor).
			Italic(true)

	// errorColor for error messages.
	errorColor = lipgloss.ANSIColor(9) // Bright red

	// errorStyle styles error messages.
	errorStyle = lipgloss.NewStyle().
			Foreground(errorColor).
			Bold(true)

	// toolCollapsedStyle styles the collapsed tool header (completed successfully).
	toolCollapsedStyle = lipgloss.NewStyle().
				Foreground(successColor)
)

// calculatePickerScrollOffset determines the scroll position for a picker list.
// It keeps the selected item visible within the visible window.
func calculatePickerScrollOffset(selectedIndex, totalItems, maxVisible int) int {
	if totalItems <= maxVisible {
		return 0
	}
	scrollOffset := 0
	if selectedIndex >= scrollOffset+maxVisible {
		scrollOffset = selectedIndex - maxVisible + 1
	}
	if selectedIndex < scrollOffset {
		scrollOffset = selectedIndex
	}
	if scrollOffset > totalItems-maxVisible {
		scrollOffset = totalItems - maxVisible
	}
	if scrollOffset < 0 {
		scrollOffset = 0
	}
	return scrollOffset
}

// calculatePickerContentLines calculates the number of content lines for a picker modal.
func calculatePickerContentLines(visibleCount int, isScrollable bool) int {
	contentLines := 1 + visibleCount
	if isScrollable {
		contentLines += 2
	}
	return max(contentLines, 6)
}

// createPickerModalStyle creates a consistent modal style for picker dialogs.
func createPickerModalStyle(width, height int) lipgloss.Style {
	return lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(primaryColor).
		PaddingLeft(1).
		PaddingRight(1).
		Width(width).
		Height(height)
}

// renderPickerModal finalizes and renders a picker modal with consistent styling.
func renderPickerModal(content string, modalWidth, visibleCount int, isScrollable bool) string {
	contentLines := calculatePickerContentLines(visibleCount, isScrollable)
	modalStyle := createPickerModalStyle(modalWidth, contentLines)
	return modalStyle.Render(content)
}
