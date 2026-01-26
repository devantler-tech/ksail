package chat

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/mitchellh/go-wordwrap"
)

// updateViewportContent updates the viewport with rendered message content.
func (m *Model) updateViewportContent() {
	if len(m.messages) == 0 {
		welcomeMsg := "  Type a message below to start chatting with KSail AI.\n"
		m.viewport.SetContent(statusStyle.Render(welcomeMsg))
		return
	}

	wrapWidth := m.calculateWrapWidth()
	var builder strings.Builder

	for _, msg := range m.messages {
		m.renderMessage(&builder, &msg, wrapWidth)
	}

	m.viewport.SetContent(builder.String())
	if !m.userScrolled {
		m.viewport.GotoBottom()
	}
}

// calculateWrapWidth calculates the content width for text wrapping.
func (m *Model) calculateWrapWidth() uint {
	wrapWidth := max(m.viewport.Width-4, 20)
	return uint(wrapWidth) //nolint:gosec // wrapWidth is guaranteed >= 20
}

// renderMessage renders a single message to the builder.
func (m *Model) renderMessage(builder *strings.Builder, msg *chatMessage, wrapWidth uint) {
	switch msg.role {
	case "user":
		m.renderUserMessage(builder, msg, wrapWidth)
	case "assistant":
		m.renderAssistantMessage(builder, msg, wrapWidth)
	case "tool":
		// Skip - tools are now rendered inline with assistant messages
	case "tool-output":
		m.renderLegacyToolOutput(builder, msg, wrapWidth)
	}
}

// renderUserMessage renders a user message.
func (m *Model) renderUserMessage(builder *strings.Builder, msg *chatMessage, wrapWidth uint) {
	builder.WriteString("\n")
	builder.WriteString(userMsgStyle.Render("▶ You"))
	builder.WriteString("\n\n")

	wrapped := wordwrap.WrapString(msg.content, wrapWidth)
	for line := range strings.SplitSeq(wrapped, "\n") {
		builder.WriteString("  ")
		builder.WriteString(line)
		builder.WriteString("\n")
	}
}

// renderAssistantMessage renders an assistant message with interleaved tools.
func (m *Model) renderAssistantMessage(builder *strings.Builder, msg *chatMessage, wrapWidth uint) {
	builder.WriteString("\n")
	builder.WriteString(assistantMsgStyle.Render("▶ KSail"))
	if msg.isStreaming {
		builder.WriteString(" " + m.spinner.View())
	}
	builder.WriteString("\n\n")

	tools := m.getToolsForMessage(msg)
	m.renderAssistantContent(builder, msg, tools, wrapWidth)

	if msg.isStreaming {
		builder.WriteString("  ▌")
	}
	builder.WriteString("\n")
}

// getToolsForMessage returns the tools to render for a message.
func (m *Model) getToolsForMessage(msg *chatMessage) []*toolExecution {
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
	msg *chatMessage,
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
	msg *chatMessage,
	content string,
	lastPos int,
	wrapWidth uint,
) {
	// Use pre-rendered markdown when no tools were interleaved and message is complete
	if lastPos == 0 && !msg.isStreaming && msg.rendered != "" {
		builder.WriteString(msg.rendered)
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
		builder.WriteString(msg.rendered)
		return
	}

	m.renderTextSegment(builder, remainingText, wrapWidth)
}

// renderLegacyToolOutput renders legacy tool output for backward compatibility.
func (m *Model) renderLegacyToolOutput(builder *strings.Builder, msg *chatMessage, wrapWidth uint) {
	wrapped := wordwrap.WrapString(msg.content, wrapWidth-2)
	for line := range strings.SplitSeq(wrapped, "\n") {
		builder.WriteString(toolOutputStyle.Render("    " + line))
		builder.WriteString("\n")
	}
}

// renderToolInline renders a tool execution inline within an assistant response.
func (m *Model) renderToolInline(builder *strings.Builder, tool *toolExecution, wrapWidth uint) {
	humanName := humanizeToolName(tool.name)
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
	builder.WriteString(toolMsgStyle.Render(line))
	builder.WriteString("\n")
	if tool.output != "" {
		m.renderToolOutput(builder, tool.output, wrapWidth, true)
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
		line := fmt.Sprintf("  ✓ %s", displayName)
		builder.WriteString(toolCollapsedStyle.Render(line))
		builder.WriteString("\n")
		if tool.output != "" {
			m.renderToolOutput(builder, tool.output, wrapWidth, true)
		}
	} else {
		line := fmt.Sprintf("  ✓ %s", displayName)
		if summary != "" {
			line += toolOutputStyle.Render(" — " + summary)
		}
		builder.WriteString(toolCollapsedStyle.Render(line))
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
	line := fmt.Sprintf("  ✗ %s", displayName)

	if tool.expanded {
		builder.WriteString(errorStyle.Render(line))
		builder.WriteString("\n")
		if tool.output != "" {
			m.renderToolOutput(builder, tool.output, wrapWidth, true)
		}
		return
	}

	// Collapsed: show first line of error as summary
	if tool.output != "" {
		line += toolOutputStyle.Render(" — " + m.truncateLine(tool.output, 50))
	}
	builder.WriteString(errorStyle.Render(line))
	builder.WriteString("\n")
}

// getToolSummary returns a brief summary of tool output for collapsed view.
func (m *Model) getToolSummary(tool *toolExecution) string {
	if tool.output == "" {
		return ""
	}

	output := strings.TrimSpace(tool.output)
	lines := strings.Split(output, "\n")

	if len(output) < 60 && len(lines) == 1 {
		return output
	}

	firstLine := strings.TrimSpace(lines[0])
	if utf8.RuneCountInString(firstLine) > 60 {
		runes := []rune(firstLine)
		firstLine = string(runes[:60]) + "..."
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
	expanded bool,
) {
	const maxOutputLines = 10
	lines := strings.Split(output, "\n")
	truncated := false

	if !expanded && len(lines) > maxOutputLines {
		lines = lines[:maxOutputLines]
		truncated = true
	}

	output = strings.Join(lines, "\n")
	wrapped := wordwrap.WrapString(output, wrapWidth-6)

	for line := range strings.SplitSeq(wrapped, "\n") {
		builder.WriteString(toolOutputStyle.Render("      " + line))
		builder.WriteString("\n")
	}

	if truncated {
		builder.WriteString(
			toolOutputStyle.Render(
				fmt.Sprintf("      ... (%d more lines, press ⇥ for full output)",
					len(strings.Split(output, "\n"))-maxOutputLines),
			),
		)
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
