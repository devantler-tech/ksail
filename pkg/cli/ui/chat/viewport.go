package chat

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/mitchellh/go-wordwrap"
)

const (
	// toolOutputTruncateLen is the maximum runes for collapsed tool output summary.
	toolOutputTruncateLen = 50
	// firstLineTruncateLen is the maximum runes for tool summary first line.
	firstLineTruncateLen = 60
	// wrapPadding is subtracted from viewport width for text wrapping.
	wrapPadding = 4
	// legacyToolIndent is padding subtracted for legacy tool output wrapping.
	legacyToolIndent = 2
	// toolOutputIndent is padding subtracted and prepended for tool output wrapping.
	toolOutputIndent = 6
)

// updateViewportContent updates the viewport with rendered message content.
func (m *Model) updateViewportContent() {
	wrapWidth := m.calculateWrapWidth()

	var builder strings.Builder

	// Render conversation messages
	if len(m.messages) == 0 {
		welcomeMsg := "  " + m.theme.WelcomeMessage + "\n"
		builder.WriteString(m.styles.status.Render(welcomeMsg))
	} else {
		for _, msg := range m.messages {
			m.renderMessage(&builder, &msg, wrapWidth)
		}
	}

	// Render pending prompts section
	if m.hasPendingPrompts() {
		m.renderPendingPrompts(&builder, wrapWidth)
	}

	m.viewport.SetContent(builder.String())

	if !m.userScrolled {
		m.viewport.GotoBottom()
	}
}

// calculateWrapWidth calculates the content width for text wrapping.
func (m *Model) calculateWrapWidth() uint {
	wrapWidth := max(m.viewport.Width-wrapPadding, minWrapWidth)

	return uint(wrapWidth) //nolint:gosec // wrapWidth is guaranteed >= minWrapWidth
}

// renderMessage renders a single message to the builder.
func (m *Model) renderMessage(builder *strings.Builder, msg *message, wrapWidth uint) {
	switch msg.role {
	case roleUser:
		m.renderUserMessage(builder, msg, wrapWidth)
	case roleAssistant:
		m.renderAssistantMessage(builder, msg, wrapWidth)
	case "tool":
		// Skip - tools are now rendered inline with assistant messages
	case "tool-output":
		m.renderLegacyToolOutput(builder, msg, wrapWidth)
	}
}

// renderUserMessage renders a user message.
func (m *Model) renderUserMessage(builder *strings.Builder, msg *message, wrapWidth uint) {
	builder.WriteString("\n")
	// Add mode indicator with icon and label
	builder.WriteString(m.styles.userMsg.Render("▶ You " + msg.chatMode.Label()))
	builder.WriteString("\n\n")

	wrapped := wordwrap.WrapString(msg.content, wrapWidth)
	for line := range strings.SplitSeq(wrapped, "\n") {
		builder.WriteString("  ")
		builder.WriteString(line)
		builder.WriteString("\n")
	}
}

// renderAssistantMessage renders an assistant message with interleaved tools.
func (m *Model) renderAssistantMessage(builder *strings.Builder, msg *message, wrapWidth uint) {
	builder.WriteString("\n")
	builder.WriteString(m.styles.assistantMsg.Render(m.theme.AssistantLabel))

	if msg.isStreaming {
		builder.WriteString(" " + m.spinner.View())
	}

	builder.WriteString("\n")

	tools := m.getToolsForMessage(msg)
	m.renderAssistantContent(builder, msg, tools, wrapWidth)

	if msg.isStreaming {
		builder.WriteString("  ▌")
	}

	builder.WriteString("\n")
}

// getToolsForMessage returns the tools to render for a message.
func (m *Model) getToolsForMessage(msg *message) []*toolExecution {
	if msg.isStreaming {
		tools := make([]*toolExecution, 0, len(m.toolOrder))
		for _, id := range m.toolOrder {
			if tool := m.tools[id]; tool != nil {
				tools = append(tools, tool)
			}
		}

		return tools
	}

	return msg.tools
}

// renderAssistantContent renders assistant text with interleaved tools.
func (m *Model) renderAssistantContent(
	builder *strings.Builder,
	msg *message,
	tools []*toolExecution,
	wrapWidth uint,
) {
	content := msg.content
	lastPos := 0

	for _, tool := range tools {
		if tool == nil {
			continue
		}

		if tool.textPosition > lastPos && tool.textPosition <= len(content) {
			m.renderTextSegment(builder, content[lastPos:tool.textPosition], wrapWidth)
			lastPos = tool.textPosition
		}

		m.renderToolInline(builder, tool, wrapWidth)
	}

	m.renderRemainingText(builder, msg, content, lastPos, wrapWidth)
}

// renderTextSegment renders a segment of text with wrapping.
func (m *Model) renderTextSegment(builder *strings.Builder, text string, wrapWidth uint) {
	if text == "" {
		return
	}

	wrapped := wordwrap.WrapString(text, wrapWidth)
	for line := range strings.SplitSeq(wrapped, "\n") {
		builder.WriteString("  ")
		builder.WriteString(line)
		builder.WriteString("\n")
	}
}

// renderRemainingText renders text after all tools have been rendered.
func (m *Model) renderRemainingText(
	builder *strings.Builder,
	msg *message,
	content string,
	lastPos int,
	wrapWidth uint,
) {
	// Use pre-rendered markdown when no tools were interleaved and message is complete
	if lastPos == 0 && !msg.isStreaming && msg.rendered != "" {
		m.writeIndentedContent(builder, msg.rendered)

		return
	}

	// Render remaining text after tools
	if lastPos >= len(content) {
		return
	}

	remainingText := content[lastPos:]
	if remainingText == "" {
		return
	}

	// Use pre-rendered markdown if available and no prior tools
	if !msg.isStreaming && msg.rendered != "" && lastPos == 0 {
		m.writeIndentedContent(builder, msg.rendered)

		return
	}

	m.renderTextSegment(builder, remainingText, wrapWidth)
}

// writeIndentedContent writes pre-rendered content with 2-space indent on each line.
func (m *Model) writeIndentedContent(builder *strings.Builder, content string) {
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		// Skip empty trailing line (from content ending with newline)
		if i == len(lines)-1 && line == "" {
			continue
		}

		builder.WriteString("  ")
		builder.WriteString(line)
		builder.WriteString("\n")
	}
}

// renderLegacyToolOutput renders legacy tool output for backward compatibility.
func (m *Model) renderLegacyToolOutput(builder *strings.Builder, msg *message, wrapWidth uint) {
	wrapped := wordwrap.WrapString(msg.content, wrapWidth-legacyToolIndent)
	for line := range strings.SplitSeq(wrapped, "\n") {
		builder.WriteString(m.styles.toolOutput.Render("    " + line))
		builder.WriteString("\n")
	}
}

// renderToolInline renders a tool execution inline within an assistant response.
func (m *Model) renderToolInline(builder *strings.Builder, tool *toolExecution, wrapWidth uint) {
	humanName := humanizeToolName(tool.name, m.toolDisplay.NameMappings)

	displayName := humanName
	if tool.command != "" {
		displayName = "> " + tool.command
	}

	switch tool.status {
	case toolRunning:
		m.renderRunningTool(builder, tool, displayName, wrapWidth)
	case toolSuccess:
		m.renderSuccessTool(builder, tool, displayName, wrapWidth)
	case toolFailed:
		m.renderFailedTool(builder, tool, displayName, wrapWidth)
	}
}

// renderRunningTool renders a tool that is currently running.
func (m *Model) renderRunningTool(
	builder *strings.Builder,
	tool *toolExecution,
	displayName string,
	wrapWidth uint,
) {
	line := fmt.Sprintf("  %s %s", m.spinner.View(), displayName)
	builder.WriteString(m.styles.toolMsg.Render(line))
	builder.WriteString("\n")

	if tool.output != "" {
		m.renderToolOutput(builder, tool.output, wrapWidth)
	}
}

// renderSuccessTool renders a successfully completed tool.
func (m *Model) renderSuccessTool(
	builder *strings.Builder,
	tool *toolExecution,
	displayName string,
	wrapWidth uint,
) {
	summary := m.getToolSummary(tool)
	if tool.expanded {
		line := "  ✓ " + displayName
		builder.WriteString(m.styles.toolCollapsed.Render(line))
		builder.WriteString("\n")

		if tool.output != "" {
			m.renderToolOutput(builder, tool.output, wrapWidth)
		}
	} else {
		line := "  ✓ " + displayName
		if summary != "" {
			line += m.styles.toolOutput.Render(" — " + summary)
		}

		builder.WriteString(m.styles.toolCollapsed.Render(line))
		builder.WriteString("\n")
	}
}

// renderFailedTool renders a failed tool.
func (m *Model) renderFailedTool(
	builder *strings.Builder,
	tool *toolExecution,
	displayName string,
	wrapWidth uint,
) {
	line := "  ✗ " + displayName

	if tool.expanded {
		builder.WriteString(m.styles.errMsg.Render(line))
		builder.WriteString("\n")

		if tool.output != "" {
			m.renderToolOutput(builder, tool.output, wrapWidth)
		}

		return
	}

	// Collapsed: show first line of error as summary
	if tool.output != "" {
		line += m.styles.toolOutput.Render(
			" — " + m.truncateLine(tool.output, toolOutputTruncateLen),
		)
	}

	builder.WriteString(m.styles.errMsg.Render(line))
	builder.WriteString("\n")
}

// getToolSummary returns a brief summary of tool output for collapsed view.
func (m *Model) getToolSummary(tool *toolExecution) string {
	if tool.output == "" {
		return ""
	}

	output := strings.TrimSpace(tool.output)
	lines := strings.Split(output, "\n")

	if len(output) < firstLineTruncateLen && len(lines) == 1 {
		return output
	}

	firstLine := strings.TrimSpace(lines[0])
	if utf8.RuneCountInString(firstLine) > firstLineTruncateLen {
		runes := []rune(firstLine)
		firstLine = string(runes[:firstLineTruncateLen]) + "..."
	}

	if len(lines) > 1 {
		firstLine += fmt.Sprintf(" (+%d lines)", len(lines)-1)
	}

	return firstLine
}

// renderToolOutput renders tool output with proper indentation.
func (m *Model) renderToolOutput(
	builder *strings.Builder,
	output string,
	wrapWidth uint,
) {
	lines := strings.Split(output, "\n")

	truncatedOutput := strings.Join(lines, "\n")
	wrapped := wordwrap.WrapString(truncatedOutput, wrapWidth-toolOutputIndent)

	for line := range strings.SplitSeq(wrapped, "\n") {
		builder.WriteString(m.styles.toolOutput.Render("      " + line))
		builder.WriteString("\n")
	}
}

// truncateLine returns the first line of text, truncated to maxLen runes (Unicode-safe).
func (m *Model) truncateLine(text string, maxLen int) string {
	firstLine := strings.Split(text, "\n")[0]
	if utf8.RuneCountInString(firstLine) > maxLen {
		runes := []rune(firstLine)

		return string(runes[:maxLen]) + "..."
	}

	return firstLine
}

// renderPendingPrompts renders the pending prompts section.
func (m *Model) renderPendingPrompts(builder *strings.Builder, wrapWidth uint) {
builder.WriteString("\n")
builder.WriteString(m.styles.status.Render("─── Pending Prompts ───"))
builder.WriteString("\n")

// Render steering prompts first
for i, prompt := range m.steeringPrompts {
m.renderPendingPrompt(builder, &prompt, i, true, wrapWidth)
}

// Render queued prompts
for i, prompt := range m.queuedPrompts {
// Adjust index to account for steering prompts in selection
displayIndex := i + len(m.steeringPrompts)
m.renderPendingPrompt(builder, &prompt, displayIndex, false, wrapWidth)
}
}

// renderPendingPrompt renders a single pending prompt.
func (m *Model) renderPendingPrompt(
builder *strings.Builder,
prompt *pendingPrompt,
index int,
isSteering bool,
wrapWidth uint,
) {
builder.WriteString("\n")

// Determine if this prompt is selected
isSelected := index == m.selectedPromptIndex

// Build prompt label
var label string
if isSteering {
label = "🎯 [STEERING]"
} else {
label = fmt.Sprintf("⏳ [QUEUED #%d]", index-len(m.steeringPrompts)+1)
}

// Add selection indicator
if isSelected {
label = "► " + label
} else {
label = "  " + label
}

// Add mode icon
label += " " + prompt.chatMode.Icon()

// Render label
if isSteering {
builder.WriteString(m.styles.assistantMsg.Render(label))
} else {
builder.WriteString(m.styles.userMsg.Render(label))
}

builder.WriteString("\n")

// Render prompt content (first line truncated)
truncated := m.truncateLine(prompt.content, 60)
builder.WriteString("    ")
builder.WriteString(truncated)
builder.WriteString("\n")
}
