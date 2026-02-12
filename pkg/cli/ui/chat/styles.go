package chat

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// ANSI color constants for picker and modal styling.
// These are standard terminal colors used as UI affordances (selected, warning, etc.).
const (
	ansiGray   = 8
	ansiGreen  = 10
	ansiYellow = 11
	ansiCyan   = 14
	ansiWhite  = 15

	// scrollIndicatorLines is the number of lines used for scroll indicators (top + bottom).
	scrollIndicatorLines = 2
)

// uiStyles holds all computed lipgloss styles derived from a ThemeConfig.
// Stored on the Model to allow multiple instances with different themes.
type uiStyles struct {
	logo            lipgloss.Style
	tagline         lipgloss.Style
	headerBox       lipgloss.Style
	userMsg         lipgloss.Style
	assistantMsg    lipgloss.Style
	toolMsg         lipgloss.Style
	toolOutput      lipgloss.Style
	help            lipgloss.Style
	spinner         lipgloss.Style
	viewport        lipgloss.Style
	input           lipgloss.Style
	status          lipgloss.Style
	errMsg          lipgloss.Style
	toolCollapsed   lipgloss.Style
	scrollIndicator lipgloss.Style
	helpKey         lipgloss.Style
	helpDesc        lipgloss.Style
}

// newUIStyles creates a full set of UI styles from the given theme configuration.
func newUIStyles(theme ThemeConfig) uiStyles {
	return uiStyles{
		logo: lipgloss.NewStyle().
			Foreground(theme.PrimaryColor).
			Bold(true),
		tagline: lipgloss.NewStyle().
			Foreground(theme.AccentColor).
			Italic(true),
		headerBox: lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(theme.PrimaryColor).
			Padding(0, modalPadding),
		userMsg: lipgloss.NewStyle().
			Foreground(theme.UserColor).
			Bold(true),
		assistantMsg: lipgloss.NewStyle().
			Foreground(theme.AssistantColor).
			Bold(true),
		toolMsg: lipgloss.NewStyle().
			Foreground(theme.ToolColor),
		toolOutput: lipgloss.NewStyle().
			Foreground(theme.DimColor),
		help: lipgloss.NewStyle().
			Foreground(theme.DimColor),
		spinner: lipgloss.NewStyle().
			Foreground(theme.AccentColor),
		viewport: lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(theme.SecondaryColor).
			Padding(0, 1),
		input: lipgloss.NewStyle().
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(theme.PrimaryColor).
			Padding(0, 1),
		status: lipgloss.NewStyle().
			Foreground(theme.DimColor).
			Italic(true),
		errMsg: lipgloss.NewStyle().
			Foreground(theme.ErrorColor).
			Bold(true),
		toolCollapsed: lipgloss.NewStyle().
			Foreground(theme.SuccessColor),
		scrollIndicator: lipgloss.NewStyle().
			Foreground(lipgloss.ANSIColor(ansiGray)),
		helpKey: lipgloss.NewStyle().
			Foreground(theme.ToolColor),
		helpDesc: lipgloss.NewStyle().
			Foreground(theme.DimColor),
	}
}

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
		contentLines += scrollIndicatorLines
	}

	return max(contentLines, minPickerHeight)
}

// createPickerModalStyle creates a consistent modal style for picker dialogs.
func (m *Model) createPickerModalStyle(width, height int) lipgloss.Style {
	return lipgloss.NewStyle().
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(m.theme.PrimaryColor).
		PaddingLeft(1).
		PaddingRight(1).
		Width(width).
		Height(height)
}

// renderPickerModal finalizes and renders a picker modal with consistent styling.
func (m *Model) renderPickerModal(
	content string,
	modalWidth, visibleCount int,
	isScrollable bool,
) string {
	contentLines := calculatePickerContentLines(visibleCount, isScrollable)
	modalStyle := m.createPickerModalStyle(modalWidth, contentLines)

	return modalStyle.Render(content)
}

// pickerTitleRenderer renders the title/filter section of a picker modal.
type pickerTitleRenderer func(*strings.Builder, lipgloss.Style)

// pickerItemsRenderer renders the items section of a picker modal.
type pickerItemsRenderer func(*strings.Builder, lipgloss.Style, int, int)

// renderPickerModalContent renders the common structure shared by all picker modals.
// It handles layout computation, scroll indicators, and delegates to the provided
// title and items renderers for type-specific content.
func (m *Model) renderPickerModalContent(
	totalItems int,
	pickerIndex int,
	renderTitle pickerTitleRenderer,
	renderItems pickerItemsRenderer,
) string {
	modalWidth := max(m.width-modalPadding, 1)
	contentWidth := max(modalWidth-contentPadding, 1)
	clipStyle := lipgloss.NewStyle().MaxWidth(contentWidth).Inline(true)

	maxVisible := m.calculateMaxPickerVisible()
	visibleCount := min(totalItems, maxVisible)

	scrollOffset := calculatePickerScrollOffset(pickerIndex, totalItems, maxVisible)

	var listContent strings.Builder
	renderTitle(&listContent, clipStyle)

	isScrollable := totalItems > maxVisible
	m.renderScrollIndicatorTop(&listContent, clipStyle, isScrollable, scrollOffset)

	endIdx := min(scrollOffset+visibleCount, totalItems)
	renderItems(&listContent, clipStyle, scrollOffset, endIdx)

	m.renderScrollIndicatorBottom(&listContent, clipStyle, isScrollable, endIdx, totalItems)

	content := strings.TrimRight(listContent.String(), "\n")

	return m.renderPickerModal(content, modalWidth, visibleCount, isScrollable)
}

// renderScrollIndicatorTop renders the "more above" indicator for a picker.
func (m *Model) renderScrollIndicatorTop(
	listContent *strings.Builder,
	clipStyle lipgloss.Style,
	isScrollable bool,
	scrollOffset int,
) {
	if isScrollable && scrollOffset > 0 {
		listContent.WriteString(clipStyle.Render(
			m.styles.scrollIndicator.Render("  ↑ more above"),
		) + "\n")
	}
}

// renderScrollIndicatorBottom renders the "more below" indicator for a picker.
func (m *Model) renderScrollIndicatorBottom(
	listContent *strings.Builder,
	clipStyle lipgloss.Style,
	isScrollable bool,
	endIdx, totalItems int,
) {
	if isScrollable && endIdx < totalItems {
		listContent.WriteString(clipStyle.Render(
			m.styles.scrollIndicator.Render("  ↓ more below"),
		))
	}
}
