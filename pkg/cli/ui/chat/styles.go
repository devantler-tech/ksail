package chat

import (
	"strings"

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
	// Color palette - uses AdaptiveColor for light/dark theme support.
	// Light color is used on dark backgrounds, Dark color on light backgrounds.
	// Falls back to ANSI colors for maximum compatibility.

	// Primary color - cyan family, used for main accents.
	primaryColor = lipgloss.AdaptiveColor{Light: "#0891b2", Dark: "#22d3ee"} // cyan-600 / cyan-400

	// Accent color - slightly muted cyan.
	accentColor = lipgloss.AdaptiveColor{Light: "#0e7490", Dark: "#67e8f9"} // cyan-700 / cyan-300

	// Secondary color - gray for borders and muted elements.
	secondaryColor = lipgloss.AdaptiveColor{
		Light: "#6b7280",
		Dark:  "#9ca3af",
	} // gray-500 / gray-400

	// User message color - blue family.
	userColor = lipgloss.AdaptiveColor{Light: "#2563eb", Dark: "#60a5fa"} // blue-600 / blue-400

	// Assistant message color - purple/magenta family.
	assistantColor = lipgloss.AdaptiveColor{
		Light: "#9333ea",
		Dark:  "#c084fc",
	} // purple-600 / purple-400

	// Tool color - yellow/amber family.
	toolColor = lipgloss.AdaptiveColor{Light: "#d97706", Dark: "#fbbf24"} // amber-600 / amber-400

	// Success color - green family.
	successColor = lipgloss.AdaptiveColor{
		Light: "#16a34a",
		Dark:  "#4ade80",
	} // green-600 / green-400

	// Dim color - muted gray for less important text.
	dimColor = lipgloss.AdaptiveColor{Light: "#9ca3af", Dark: "#6b7280"} // gray-400 / gray-500

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
			Foreground(dimColor) // Muted gray for subtle help text

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

	// errorColor for error messages - red family.
	errorColor = lipgloss.AdaptiveColor{Light: "#dc2626", Dark: "#f87171"} // red-600 / red-400

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

// scrollIndicatorStyle is the style for scroll indicators in pickers.
var scrollIndicatorStyle = lipgloss.NewStyle().Foreground(lipgloss.ANSIColor(8))

// renderScrollIndicatorTop renders the "more above" indicator for a picker.
func renderScrollIndicatorTop(
	listContent *strings.Builder,
	clipStyle lipgloss.Style,
	isScrollable bool,
	scrollOffset int,
) {
	if isScrollable && scrollOffset > 0 {
		listContent.WriteString(clipStyle.Render(
			scrollIndicatorStyle.Render("  ↑ more above"),
		) + "\n")
	}
}

// renderScrollIndicatorBottom renders the "more below" indicator for a picker.
func renderScrollIndicatorBottom(
	listContent *strings.Builder,
	clipStyle lipgloss.Style,
	isScrollable bool,
	endIdx, totalItems int,
) {
	if isScrollable && endIdx < totalItems {
		listContent.WriteString(clipStyle.Render(
			scrollIndicatorStyle.Render("  ↓ more below"),
		))
	}
}
