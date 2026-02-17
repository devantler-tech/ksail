package chat

import (
	"strings"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/lipgloss"
)

// Help key symbols for consistent rendering.
const (
	enterSymbol = "⏎"
	keyEscape   = "esc"
	keyArrows   = "↑↓"
	keyPageNav  = "PgUp/Dn"
	helpSep     = " • "

	// Key string constants for keyboard event matching.
	keyEnter     = "enter"
	keyCtrlC     = "ctrl+c"
	keyDown      = "down"
	keyBackspace = "backspace"

	// UI dimension constants.
	modalPadding   = 2 // border width subtracted from terminal width
	contentPadding = 4 // padding inside modal content area
	helpSepSpacing = 3 // spacing for help separator

	// Header and layout constants.
	headerPadding         = 6 // padding around header content
	textAreaPadding       = 6 // padding subtracted from width for textarea
	viewportWidthPadding  = 4 // horizontal padding subtracted from width for viewport
	viewportHeightPadding = 4 // vertical padding subtracted from height for viewport
	viewportInner         = 2 // inner padding inside viewport
	rendererPadding       = 8 // padding subtracted from width for markdown renderer
	rendererMinWidth      = 4 // padding subtracted from viewport width for recreation
	charLimit             = 4096
	eventChanBuf          = 100 // buffer size for event channel
	scrollLines           = 3   // number of lines to scroll per mouse wheel event
	minHeight             = 5   // minimum viewport height
	minSpacing            = 2   // minimum spacing for tagline row

	// Permission modal line counts.
	permissionBaseLines = 6 // title + blank + tool + blank + "Allow?" + buttons
	pickerOverhead      = 3 // title + top/bottom padding
	minPickerHeight     = 6 // minimum content lines for picker modal

	// Help layout constants.
	minHelpWidth = 20 // minimum width for help footer rendering

	// Model and role constants.
	modelAuto       = "auto"
	checkmarkSuffix = " ✓"
	roleUser        = "user"
	roleAssistant   = "assistant"

	// Auto model selection discount.
	// Paid Copilot plans receive a 10% multiplier discount when using auto model selection.
	// See: https://docs.github.com/en/copilot/concepts/auto-model-selection#multiplier-discounts
	autoDiscountFactor = 0.9
)

// createHelpModel creates a configured help model.
func createHelpModel(styles uiStyles) help.Model {
	helpModel := help.New()
	helpModel.ShortSeparator = helpSep
	helpModel.FullSeparator = "   "
	helpModel.Ellipsis = "…"
	helpModel.Styles = help.Styles{
		ShortKey:       styles.helpKey,
		ShortDesc:      styles.helpDesc,
		ShortSeparator: styles.help,
		Ellipsis:       styles.help,
		FullKey:        styles.helpKey,
		FullDesc:       styles.helpDesc,
		FullSeparator:  styles.help,
	}

	return helpModel
}

// renderHelpOverlay renders the full help overlay modal matching the input area size.
func (m *Model) renderHelpOverlay() string {
	modalWidth := max(m.width-modalPadding, 1)
	contentWidth := max(modalWidth-contentPadding, 1)

	parts := m.helpOverlayParts()

	needed := countFlowLines(parts, contentWidth)
	contentLines := min(needed, m.maxHelpContentLines())
	contentLines = max(contentLines, inputHeight)
	contentStr := flowHelpParts(parts, contentWidth, contentLines)
	modalStyle := m.createPickerModalStyle(modalWidth, contentLines)

	return modalStyle.Render(contentStr)
}

// helpOverlayParts returns all help keybindings in display order.
func (m *Model) helpOverlayParts() []string {
	return []string{
		m.styles.helpKey.Render(enterSymbol) + " send",
		m.styles.helpKey.Render("Alt+"+enterSymbol) + " newline",
		m.styles.helpKey.Render(keyArrows) + " history",
		m.styles.helpKey.Render(keyPageNav) + " scroll",
		m.styles.helpKey.Render("Tab") + " mode",
		m.styles.helpKey.Render("^Y") + " auto-approve",
		m.styles.helpKey.Render("^T") + " tools",
		m.styles.helpKey.Render("^R") + " copy",
		m.styles.helpKey.Render("^H") + " sessions",
		m.styles.helpKey.Render("^O") + " model",
		m.styles.helpKey.Render("^E") + " effort",
		m.styles.helpKey.Render("^N") + " new",
		m.styles.helpKey.Render(keyEscape) + " close",
		m.styles.helpKey.Render("^C") + " quit",
	}
}

// maxHelpContentLines returns the maximum content lines the help overlay
// can use without pushing the layout outside the terminal viewport.
func (m *Model) maxHelpContentLines() int {
	// modalBorderLines accounts for the top+bottom border of the modal box.
	const modalBorderLines = 2

	avail := m.height - m.headerHeight - footerHeight - viewportHeightPadding - minHeight - modalBorderLines

	return max(avail, 1)
}

// helpOverlayContentLines returns the number of content lines the help overlay
// will occupy at the current terminal width.
func (m *Model) helpOverlayContentLines() int {
	modalWidth := max(m.width-modalPadding, 1)
	contentWidth := max(modalWidth-contentPadding, 1)
	parts := m.helpOverlayParts()
	needed := countFlowLines(parts, contentWidth)

	return max(min(needed, m.maxHelpContentLines()), 1)
}

// helpOverlayExtraHeight returns extra height beyond inputHeight for layout.
func (m *Model) helpOverlayExtraHeight() int {
	lines := m.helpOverlayContentLines()
	if lines > inputHeight {
		return lines - inputHeight
	}

	return 0
}

// countFlowLines counts how many lines are needed to flow parts within maxWidth.
func countFlowLines(parts []string, maxWidth int) int {
	// Use a very large maxLines so flowHelpParts doesn't truncate.
	result := flowHelpParts(parts, maxWidth, len(parts))

	return strings.Count(result, "\n") + 1
}

// flowHelpParts arranges help parts into lines that wrap within maxWidth,
// returning at most maxLines lines. Parts that don't fit are omitted.
func flowHelpParts(parts []string, maxWidth, maxLines int) string {
	var lines []string

	var line strings.Builder

	lineWidth := 0
	sepWidth := lipgloss.Width(helpSep)

	for _, part := range parts {
		partWidth := lipgloss.Width(part)
		needsSep := lineWidth > 0

		totalNeeded := partWidth
		if needsSep {
			totalNeeded += sepWidth
		}

		if needsSep && lineWidth+totalNeeded > maxWidth {
			lines = append(lines, line.String())
			if len(lines) >= maxLines {
				break
			}

			line.Reset()

			lineWidth = 0
			needsSep = false
		}

		if needsSep {
			line.WriteString(helpSep)

			lineWidth += sepWidth
		}

		line.WriteString(part)

		lineWidth += partWidth
	}

	if line.Len() > 0 && len(lines) < maxLines {
		lines = append(lines, line.String())
	}

	return strings.Join(lines, "\n")
}

// renderShortHelp renders the context-aware footer help text using the KeyMap.
// It intelligently truncates to fit available width while always showing "F1 help".
func (m *Model) renderShortHelp() string {
	availWidth := max(m.width-contentPadding, minHelpWidth) // Account for padding
	helpToggle := m.styles.helpKey.Render("F1") + " help"
	helpToggleWidth := lipgloss.Width(helpToggle)
	usableWidth := availWidth - helpToggleWidth - helpSepSpacing // Space for separator

	parts := m.getContextHelpParts()
	result := buildTruncatedHelp(parts, usableWidth)

	var finalHelp strings.Builder
	if result != "" {
		finalHelp.WriteString(result)
		finalHelp.WriteString(helpSep)
	}

	finalHelp.WriteString(helpToggle)

	return m.styles.help.Render("  " + finalHelp.String())
}

// getContextHelpParts returns help parts based on current UI context.
func (m *Model) getContextHelpParts() []string {
	switch {
	case m.showHelpOverlay:
		return []string{m.styles.helpKey.Render(keyEscape) + " close"}

	case m.pendingPermission != nil:
		return getPermissionHelpParts(m.styles.helpKey)

	case m.showModelPicker:
		return m.getModelPickerHelpParts()

	case m.showReasoningPicker:
		return m.getReasoningPickerHelpParts()

	case m.showSessionPicker:
		return m.getSessionPickerHelpParts()

	default:
		return m.getDefaultHelpParts()
	}
}

// getPermissionHelpParts returns help for permission prompts.
func getPermissionHelpParts(helpKeyStyle lipgloss.Style) []string {
	return []string{
		helpKeyStyle.Render("y") + " allow",
		helpKeyStyle.Render("n") + " deny",
		helpKeyStyle.Render("a") + " always",
	}
}

// getModelPickerHelpParts returns help for model picker state.
func (m *Model) getModelPickerHelpParts() []string {
	if m.modelFilterActive {
		return getFilterModeHelpParts(m.styles.helpKey)
	}

	return []string{
		m.styles.helpKey.Render(keyArrows) + " select",
		m.styles.helpKey.Render("/") + " filter",
		m.styles.helpKey.Render(enterSymbol) + " confirm",
		m.styles.helpKey.Render(keyEscape) + " cancel",
	}
}

// getReasoningPickerHelpParts returns help for reasoning effort picker state.
func (m *Model) getReasoningPickerHelpParts() []string {
	return []string{
		m.styles.helpKey.Render(keyArrows) + " select",
		m.styles.helpKey.Render(enterSymbol) + " confirm",
		m.styles.helpKey.Render(keyEscape) + " cancel",
	}
}

// getSessionPickerHelpParts returns help for session picker state.
func (m *Model) getSessionPickerHelpParts() []string {
	switch {
	case m.renamingSession:
		return []string{
			m.styles.helpKey.Render(enterSymbol) + " save",
			m.styles.helpKey.Render(keyEscape) + " cancel",
		}
	case m.confirmDeleteSession:
		return []string{
			m.styles.helpKey.Render("y") + " delete",
			m.styles.helpKey.Render("n") + " cancel",
		}
	case m.sessionFilterActive:
		return getFilterModeHelpParts(m.styles.helpKey)
	default:
		return []string{
			m.styles.helpKey.Render(keyArrows) + " select",
			m.styles.helpKey.Render("/") + " filter",
			m.styles.helpKey.Render("r") + " rename",
			m.styles.helpKey.Render("d") + " delete",
			m.styles.helpKey.Render(keyEscape) + " close",
		}
	}
}

// getDefaultHelpParts returns help for the default chat view.
func (m *Model) getDefaultHelpParts() []string {
	parts := []string{
		m.styles.helpKey.Render(enterSymbol) + " send",
	}

	// Conditionally add copy hint
	if m.hasAssistantMessages() {
		parts = append(parts, m.styles.helpKey.Render("^R")+" copy")
	}

	parts = append(parts, m.styles.helpKey.Render("Tab")+" "+m.chatMode.Label())

	// Add auto-approve mode hint
	autoApproveHint := "auto-approve"
	if m.yoloMode {
		autoApproveHint = "auto-approve ✓"
	}

	parts = append(parts, m.styles.helpKey.Render("^Y")+" "+autoApproveHint)

	// Conditionally add tools hint
	if len(m.toolOrder) > 0 || m.hasToolsInMessages() {
		parts = append(parts, m.styles.helpKey.Render("^T")+" tools")
	}

	parts = append(parts, m.styles.helpKey.Render("^H")+" sessions")
	parts = append(parts, m.styles.helpKey.Render("^O")+" model")

	// Conditionally add reasoning effort hint
	if m.currentModelSupportsReasoning() || m.sessionConfig.ReasoningEffort != "" {
		effortLabel := "effort"
		if m.sessionConfig.ReasoningEffort != "" {
			effortLabel = m.sessionConfig.ReasoningEffort
		}

		parts = append(parts, m.styles.helpKey.Render("^E")+" "+effortLabel)
	}

	parts = append(parts, m.styles.helpKey.Render("^N")+" new")

	return parts
}

// buildTruncatedHelp builds a help string that fits within maxWidth.
func buildTruncatedHelp(parts []string, maxWidth int) string {
	var result strings.Builder

	currentWidth := 0

	for idx, part := range parts {
		partWidth := lipgloss.Width(part)

		sepWidth := 0
		if idx > 0 {
			sepWidth = lipgloss.Width(helpSep)
		}

		if currentWidth+sepWidth+partWidth > maxWidth {
			break
		}

		if idx > 0 {
			result.WriteString(helpSep)
		}

		result.WriteString(part)

		currentWidth += sepWidth + partWidth
	}

	return result.String()
}

// getFilterModeHelpParts returns help parts for filter mode (used by both model and session pickers).
func getFilterModeHelpParts(helpKeyStyle lipgloss.Style) []string {
	return []string{
		helpKeyStyle.Render(enterSymbol) + " done",
		helpKeyStyle.Render(keyEscape) + " clear",
	}
}

// hasToolsInMessages checks if any messages have tools.
func (m *Model) hasToolsInMessages() bool {
	for _, msg := range m.messages {
		if msg.role == roleAssistant && len(msg.tools) > 0 {
			return true
		}
	}

	return false
}

// hasAssistantMessages checks if there are any completed assistant messages.
func (m *Model) hasAssistantMessages() bool {
	for _, msg := range m.messages {
		if msg.role == roleAssistant && msg.content != "" && !msg.isStreaming {
			return true
		}
	}

	return false
}
