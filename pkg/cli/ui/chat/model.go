package chat

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	copilot "github.com/github/copilot-sdk/go"
	"github.com/mitchellh/go-wordwrap"
)

const (
	defaultWidth  = 100
	defaultHeight = 30
	inputHeight   = 3
	headerHeight  = logoHeight + 3 // logo + tagline + border
	footerHeight  = 2
)

// chatMessage represents a single message in the chat history.
type chatMessage struct {
	role        string // "user", "assistant", or "tool"
	content     string
	rendered    string // markdown-rendered content for assistant messages
	isStreaming bool
}

// toolStatus represents the current state of a tool execution.
type toolStatus int

const (
	toolRunning toolStatus = iota
	toolSuccess
	toolFailed
)

// toolExecution tracks a single tool invocation.
type toolExecution struct {
	id        string
	name      string
	status    toolStatus
	output    string
	expanded  bool // whether output is expanded in the view
	startTime time.Time
}

// humanizeToolName converts snake_case tool names to readable format.
func humanizeToolName(name string) string {
	// Common tool name mappings for better readability
	mappings := map[string]string{
		"report_intent":        "Analyzing request",
		"ksail_cluster_list":   "Listing clusters",
		"ksail_cluster_info":   "Getting cluster info",
		"ksail_cluster_create": "Creating cluster",
		"ksail_cluster_delete": "Deleting cluster",
		"bash":                 "Running command",
		"read_file":            "Reading file",
		"write_file":           "Writing file",
		"list_dir":             "Listing directory",
	}
	if mapped, ok := mappings[name]; ok {
		return mapped
	}
	// Fallback: convert snake_case to Title Case
	words := strings.Split(name, "_")
	for i, word := range words {
		if len(word) > 0 {
			words[i] = strings.ToUpper(word[:1]) + word[1:]
		}
	}
	return strings.Join(words, " ")
}

// Model is the Bubbletea model for the chat TUI.
type Model struct {
	// Components
	viewport viewport.Model
	textarea textarea.Model
	spinner  spinner.Model

	// State
	messages        []chatMessage
	currentResponse strings.Builder
	isStreaming     bool
	err             error
	quitting        bool
	ready           bool

	// Tool execution tracking
	tools     map[string]*toolExecution // keyed by tool ID
	toolOrder []string                  // ordered list of tool IDs for rendering

	// Dimensions
	width  int
	height int

	// Copilot session
	session *copilot.Session
	timeout time.Duration
	ctx     context.Context

	// Markdown renderer (cached to avoid terminal queries)
	renderer *glamour.TermRenderer

	// Channel for async streaming events from Copilot
	eventChan chan tea.Msg

	// Cleanup function for event subscription
	unsubscribe func()
}

// New creates a new chat TUI model.
func New(session *copilot.Session, timeout time.Duration) Model {
	// Initialize textarea for user input
	textArea := textarea.New()
	textArea.Placeholder = "Ask me anything about Kubernetes, KSail, or cluster management..."
	textArea.Focus()
	textArea.CharLimit = 4096
	textArea.SetWidth(defaultWidth - 6)
	textArea.SetHeight(inputHeight)
	textArea.ShowLineNumbers = false
	// Show ">" only on first line, nothing on continuation lines
	textArea.SetPromptFunc(2, func(lineIdx int) string {
		if lineIdx == 0 {
			return "> "
		}
		return "  "
	})
	textArea.FocusedStyle.CursorLine = lipgloss.NewStyle()
	textArea.FocusedStyle.Placeholder = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

	// Initialize viewport for chat history
	viewPort := viewport.New(defaultWidth-4, defaultHeight-inputHeight-headerHeight-footerHeight-4)
	initialMsg := "  Type a message below to start chatting with KSail AI.\n"
	viewPort.SetContent(statusStyle.Render(initialMsg))

	// Initialize spinner
	spin := spinner.New()
	spin.Spinner = spinner.MiniDot
	spin.Style = spinnerStyle

	// Initialize markdown renderer before Bubbletea takes over terminal
	// This avoids terminal queries that could interfere with input
	mdRenderer := createRenderer(defaultWidth - 8)

	return Model{
		viewport:  viewPort,
		textarea:  textArea,
		spinner:   spin,
		renderer:  mdRenderer,
		messages:  make([]chatMessage, 0),
		session:   session,
		timeout:   timeout,
		ctx:       context.Background(),
		eventChan: make(chan tea.Msg, 100),
		width:     defaultWidth,
		height:    defaultHeight,
		tools:     make(map[string]*toolExecution),
		toolOrder: make([]string, 0),
	}
}

// Init initializes the model and returns an initial command.
func (m Model) Init() tea.Cmd {
	return tea.Batch(textarea.Blink, m.spinner.Tick)
}

// Update handles messages and updates the model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKeyMsg(msg)

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.updateDimensions()
		if !m.ready {
			m.ready = true
		}

	case userSubmitMsg:
		return m.handleUserSubmit(msg)

	case streamChunkMsg:
		return m.handleStreamChunk(msg)

	case toolStartMsg:
		return m.handleToolStart(msg)

	case toolEndMsg:
		return m.handleToolEnd(msg)

	case streamEndMsg:
		return m.handleStreamEnd()

	case streamErrMsg:
		return m.handleStreamErr(msg)

	case unsubscribeMsg:
		// Store unsubscribe function for cleanup
		m.unsubscribe = msg.fn
		// Continue waiting for actual content events
		return m, m.waitForEvent()

	case spinner.TickMsg:
		// Always update spinner to keep it ticking
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		cmds = append(cmds, cmd)

	case tea.MouseMsg:
		return m.handleMouseMsg(msg)
	}

	// Update sub-components
	if !m.isStreaming {
		var taCmd tea.Cmd
		m.textarea, taCmd = m.textarea.Update(msg)
		cmds = append(cmds, taCmd)
	}

	var vpCmd tea.Cmd
	m.viewport, vpCmd = m.viewport.Update(msg)
	cmds = append(cmds, vpCmd)

	return m, tea.Batch(cmds...)
}

// View renders the TUI.
func (m Model) View() string {
	if m.quitting {
		goodbye := statusStyle.Render("  Goodbye! Thanks for using KSail.\n")
		return logoStyle.Render(logo) + "\n\n" + goodbye
	}

	sections := make([]string, 0, 4)

	// Header with logo
	headerContent := logoStyle.Render(logo) + "\n" + taglineStyle.Render("  "+tagline)
	if m.isStreaming {
		headerContent += "  " + m.spinner.View() + " " + statusStyle.Render("Thinking...")
	}
	header := headerBoxStyle.Width(m.width - 2).Render(headerContent)
	sections = append(sections, header)

	// Chat viewport
	chatContent := viewportStyle.Width(m.width - 2).Render(m.viewport.View())
	sections = append(sections, chatContent)

	// Input area
	inputContent := inputStyle.Width(m.width - 2).Render(m.textarea.View())
	sections = append(sections, inputContent)

	// Footer/help
	var helpText string
	if len(m.toolOrder) > 0 {
		helpText = "  ⏎ Send • ⌥⏎ Newline • ⇥ Toggle Output • ^L Clear • esc Quit • Powered by GitHub Copilot"
	} else {
		helpText = "  ⏎ Send • ⌥⏎ Newline • ^L Clear • esc Quit • Powered by GitHub Copilot"
	}
	help := helpStyle.Render(helpText)
	sections = append(sections, help)

	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

// handleKeyMsg handles keyboard input.
func (m Model) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		// Always quit on ctrl+c, even during streaming
		m.cleanup()
		m.quitting = true
		return m, tea.Quit
	case "esc":
		if m.isStreaming {
			// Cancel current stream
			m.cleanup()
			m.isStreaming = false
			if len(m.messages) > 0 && m.messages[len(m.messages)-1].role == "assistant" {
				m.messages[len(m.messages)-1].content += " [cancelled]"
				m.messages[len(m.messages)-1].isStreaming = false
			}
			m.updateViewportContent()
			return m, nil
		}
		m.quitting = true
		return m, tea.Quit
	case "ctrl+l":
		// Clear chat history and tool tracking
		if !m.isStreaming {
			m.messages = make([]chatMessage, 0)
			m.currentResponse.Reset()
			m.tools = make(map[string]*toolExecution)
			m.toolOrder = make([]string, 0)
			m.updateViewportContent()
		}
	case "alt+enter":
		// Insert newline (alt+enter since terminals don't detect shift+enter)
		m.textarea.InsertString("\n")
		return m, nil
	case "tab":
		// Toggle expansion of the most recent completed tool
		if !m.isStreaming && len(m.toolOrder) > 0 {
			// Find the most recent completed tool and toggle it
			for i := len(m.toolOrder) - 1; i >= 0; i-- {
				tool := m.tools[m.toolOrder[i]]
				if tool != nil && tool.status != toolRunning {
					tool.expanded = !tool.expanded
					m.updateViewportContent()
					break
				}
			}
		}
		return m, nil
	case "enter":
		// Send message on Enter
		if !m.isStreaming && strings.TrimSpace(m.textarea.Value()) != "" {
			content := m.textarea.Value()
			m.textarea.Reset()
			// Set streaming immediately so spinner shows right away
			m.isStreaming = true
			// Batch spinner tick with send command to start animation immediately
			return m, tea.Batch(m.spinner.Tick, m.sendMessageCmd(content))
		}
	}

	// Update textarea for other keys
	var taCmd tea.Cmd
	m.textarea, taCmd = m.textarea.Update(msg)
	return m, taCmd
}

// handleUserSubmit handles user message submission.
func (m Model) handleUserSubmit(msg userSubmitMsg) (tea.Model, tea.Cmd) {
	m.messages = append(m.messages, chatMessage{
		role:    "user",
		content: msg.content,
	})
	m.isStreaming = true
	m.currentResponse.Reset()
	m.messages = append(m.messages, chatMessage{
		role:        "assistant",
		content:     "",
		isStreaming: true,
	})
	m.updateViewportContent()
	// Start streaming, keep spinner ticking, and wait for events
	return m, tea.Batch(m.spinner.Tick, m.streamResponseCmd(msg.content), m.waitForEvent())
}

// handleStreamChunk handles streaming response chunks.
func (m Model) handleStreamChunk(msg streamChunkMsg) (tea.Model, tea.Cmd) {
	if msg.content != "" {
		m.currentResponse.WriteString(msg.content)
		if len(m.messages) > 0 {
			m.messages[len(m.messages)-1].content = m.currentResponse.String()
		}
		m.updateViewportContent()
	}
	return m, m.waitForEvent()
}

// handleToolStart handles tool execution start events.
func (m Model) handleToolStart(msg toolStartMsg) (tea.Model, tea.Cmd) {
	// Generate tool ID if not provided
	toolID := msg.toolID
	if toolID == "" {
		toolID = fmt.Sprintf("tool-%d", time.Now().UnixNano())
	}

	// Create tool execution entry
	tool := &toolExecution{
		id:        toolID,
		name:      msg.toolName,
		status:    toolRunning,
		expanded:  true, // expanded by default while running
		startTime: time.Now(),
	}
	m.tools[toolID] = tool
	m.toolOrder = append(m.toolOrder, toolID)

	// DON'T insert tool as separate message - render inline with assistant response
	m.updateViewportContent()
	return m, m.waitForEvent()
}

// handleToolEnd handles tool execution completion events.
func (m Model) handleToolEnd(msg toolEndMsg) (tea.Model, tea.Cmd) {
	// Find the tool by ID or by name (fallback for SDK compatibility)
	var tool *toolExecution
	if msg.toolID != "" {
		tool = m.tools[msg.toolID]
	}
	if tool == nil {
		// Fallback: find by name (last running tool with this name)
		for i := len(m.toolOrder) - 1; i >= 0; i-- {
			t := m.tools[m.toolOrder[i]]
			if t != nil && t.name == msg.toolName && t.status == toolRunning {
				tool = t
				break
			}
		}
	}

	if tool != nil {
		// Update tool status
		if msg.success {
			tool.status = toolSuccess
		} else {
			tool.status = toolFailed
		}
		tool.output = msg.output
		tool.expanded = false // collapse after completion
	}

	m.updateViewportContent()
	return m, m.waitForEvent()
}

// handleStreamEnd handles stream completion events.
func (m Model) handleStreamEnd() (tea.Model, tea.Cmd) {
	m.isStreaming = false
	m.cleanup() // Clean up event subscription
	if len(m.messages) > 0 {
		last := &m.messages[len(m.messages)-1]
		last.isStreaming = false
		last.content = m.currentResponse.String()
		// Render markdown using cached renderer (avoids terminal queries)
		last.rendered = renderMarkdownWithRenderer(m.renderer, last.content)
	}
	m.updateViewportContent()
	// Don't wait for more events - response is complete
	return m, nil
}

// handleStreamErr handles streaming error events.
func (m Model) handleStreamErr(msg streamErrMsg) (tea.Model, tea.Cmd) {
	m.isStreaming = false
	m.cleanup() // Clean up event subscription
	m.err = msg.err
	if len(m.messages) > 0 && m.messages[len(m.messages)-1].role == "assistant" {
		m.messages[len(m.messages)-1].content = fmt.Sprintf("Error: %v", msg.err)
		m.messages[len(m.messages)-1].isStreaming = false
	}
	m.updateViewportContent()
	// Don't wait for more events - response is complete (with error)
	return m, nil
}

// handleMouseMsg handles mouse events.
func (m Model) handleMouseMsg(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	// Pass mouse events to viewport for scrolling
	var vpCmd tea.Cmd
	m.viewport, vpCmd = m.viewport.Update(msg)
	return m, vpCmd
}

// waitForEvent returns a command that waits for an event from the channel.
// Uses select to allow context cancellation.
func (m Model) waitForEvent() tea.Cmd {
	ctx := m.ctx
	eventChan := m.eventChan
	return func() tea.Msg {
		select {
		case msg := <-eventChan:
			return msg
		case <-ctx.Done():
			return streamErrMsg{err: ctx.Err()}
		}
	}
}

// cleanup releases resources when streaming ends or is cancelled.
func (m *Model) cleanup() {
	if m.unsubscribe != nil {
		m.unsubscribe()
		m.unsubscribe = nil
	}
	// Drain any remaining events from the channel
	for {
		select {
		case <-m.eventChan:
			// Discard
		default:
			return
		}
	}
}

// updateDimensions updates component dimensions based on terminal size.
func (m *Model) updateDimensions() {
	// Account for borders and padding
	contentWidth := m.width - 4
	// Calculate available height: total - header - input - footer - borders/padding
	viewportHeight := m.height - headerHeight - inputHeight - footerHeight - 8

	if viewportHeight < 5 {
		viewportHeight = 5
	}

	m.viewport.Width = contentWidth - 2
	m.viewport.Height = viewportHeight
	m.textarea.SetWidth(contentWidth - 2)
}

// updateViewportContent rebuilds the viewport content from message history.
func (m *Model) updateViewportContent() {
	if len(m.messages) == 0 {
		welcomeMsg := "  Type a message below to start chatting with KSail AI.\n"
		m.viewport.SetContent(statusStyle.Render(welcomeMsg))
		return
	}

	// Calculate content width for wrapping (viewport width minus indent)
	wrapWidth := m.viewport.Width - 4
	if wrapWidth < 20 {
		wrapWidth = 20
	}
	wrapWidthUint := uint(wrapWidth) //nolint:gosec // wrapWidth is guaranteed >= 20

	var builder strings.Builder

	// Track which tools have been rendered to avoid duplicates
	toolIdx := 0

	for _, msg := range m.messages {
		switch msg.role {
		case "user":
			builder.WriteString(userMsgStyle.Render("▶ You"))
			builder.WriteString("\n")
			// Wrap user content
			wrapped := wordwrap.WrapString(msg.content, wrapWidthUint)
			for _, line := range strings.Split(wrapped, "\n") {
				builder.WriteString("  ")
				builder.WriteString(line)
				builder.WriteString("\n")
			}
			builder.WriteString("\n")

		case "assistant":
			builder.WriteString(assistantMsgStyle.Render("▶ KSail"))
			if msg.isStreaming {
				builder.WriteString(" " + m.spinner.View())
			}
			builder.WriteString("\n")

			// Render any tools that belong to this response BEFORE the text
			for toolIdx < len(m.toolOrder) {
				tool := m.tools[m.toolOrder[toolIdx]]
				if tool == nil {
					toolIdx++
					continue
				}
				m.renderToolInline(&builder, tool, wrapWidthUint)
				toolIdx++
			}

			// Now render the assistant's text content
			if msg.isStreaming {
				if msg.content != "" {
					wrapped := wordwrap.WrapString(msg.content, wrapWidthUint)
					for _, line := range strings.Split(wrapped, "\n") {
						builder.WriteString("  ")
						builder.WriteString(line)
						builder.WriteString("\n")
					}
				}
				builder.WriteString("  ▌")
			} else if msg.rendered != "" {
				builder.WriteString(msg.rendered)
			} else if msg.content != "" {
				wrapped := wordwrap.WrapString(msg.content, wrapWidthUint)
				for _, line := range strings.Split(wrapped, "\n") {
					builder.WriteString("  ")
					builder.WriteString(line)
					builder.WriteString("\n")
				}
			}
			builder.WriteString("\n")

		case "tool":
			// Skip - tools are now rendered inline with assistant messages
			continue

		case "tool-output":
			// Legacy tool output - still render for backward compatibility
			wrapped := wordwrap.WrapString(msg.content, wrapWidthUint-2)
			for _, line := range strings.Split(wrapped, "\n") {
				builder.WriteString(toolOutputStyle.Render("    " + line))
				builder.WriteString("\n")
			}
		}
	}
	m.viewport.SetContent(builder.String())
	m.viewport.GotoBottom()
}

// renderToolInline renders a tool execution inline within an assistant response.
func (m *Model) renderToolInline(builder *strings.Builder, tool *toolExecution, wrapWidth uint) {
	humanName := humanizeToolName(tool.name)

	switch tool.status {
	case toolRunning:
		// Running: show spinner with human-readable action
		line := fmt.Sprintf("  %s %s...", m.spinner.View(), humanName)
		builder.WriteString(toolMsgStyle.Render(line))
		builder.WriteString("\n")

	case toolSuccess:
		// Completed: show checkmark with summary
		summary := m.getToolSummary(tool)
		if tool.expanded {
			line := fmt.Sprintf("  ✓ %s", humanName)
			builder.WriteString(toolCollapsedStyle.Render(line))
			builder.WriteString("\n")
			// Show output when expanded
			if tool.output != "" {
				m.renderToolOutput(builder, tool.output, wrapWidth)
			}
		} else {
			// Collapsed: show name + brief summary on same line
			line := fmt.Sprintf("  ✓ %s", humanName)
			if summary != "" {
				line += toolOutputStyle.Render(" — " + summary)
			}
			builder.WriteString(toolCollapsedStyle.Render(line))
			builder.WriteString("\n")
		}

	case toolFailed:
		// Failed: show X with error info
		//nolint:nestif // nested conditions for expanded/collapsed view
		if tool.expanded {
			line := fmt.Sprintf("  ✗ %s", humanName)
			builder.WriteString(errorStyle.Render(line))
			builder.WriteString("\n")
			if tool.output != "" {
				m.renderToolOutput(builder, tool.output, wrapWidth)
			}
		} else {
			line := fmt.Sprintf("  ✗ %s", humanName)
			if tool.output != "" {
				// Show first line of error
				firstLine := strings.Split(tool.output, "\n")[0]
				if len(firstLine) > 50 {
					firstLine = firstLine[:50] + "..."
				}
				line += toolOutputStyle.Render(" — " + firstLine)
			}
			builder.WriteString(errorStyle.Render(line))
			builder.WriteString("\n")
		}
	}
}

// getToolSummary returns a brief summary of tool output for collapsed view.
func (m *Model) getToolSummary(tool *toolExecution) string {
	if tool.output == "" {
		return ""
	}

	output := strings.TrimSpace(tool.output)
	lines := strings.Split(output, "\n")

	// For short outputs, just show the whole thing
	if len(output) < 60 && len(lines) == 1 {
		return output
	}

	// Return first meaningful line, truncated
	firstLine := strings.TrimSpace(lines[0])
	if len(firstLine) > 60 {
		firstLine = firstLine[:60] + "..."
	}

	if len(lines) > 1 {
		firstLine += fmt.Sprintf(" (+%d lines)", len(lines)-1)
	}

	return firstLine
}

// renderToolOutput renders tool output with proper indentation and truncation.
func (m *Model) renderToolOutput(builder *strings.Builder, output string, wrapWidth uint) {
	const maxOutputLines = 20
	lines := strings.Split(output, "\n")
	truncated := false

	if len(lines) > maxOutputLines {
		lines = lines[:maxOutputLines]
		truncated = true
	}

	output = strings.Join(lines, "\n")
	wrapped := wordwrap.WrapString(output, wrapWidth-6)

	for _, line := range strings.Split(wrapped, "\n") {
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

// sendMessageCmd returns a command that initiates message sending.
func (m *Model) sendMessageCmd(content string) tea.Cmd {
	return func() tea.Msg {
		return userSubmitMsg{content: content}
	}
}

// streamResponseCmd creates a command that streams the Copilot response.
func (m *Model) streamResponseCmd(userMessage string) tea.Cmd {
	session := m.session
	eventChan := m.eventChan

	return func() tea.Msg {
		// Set up event handler to send events to the channel
		unsubscribe := session.On(func(event copilot.SessionEvent) {
			switch event.Type {
			case copilot.AssistantMessageDelta:
				if event.Data.DeltaContent != nil {
					eventChan <- streamChunkMsg{content: *event.Data.DeltaContent}
				}
			case copilot.SessionIdle:
				eventChan <- streamEndMsg{}
			case copilot.SessionError:
				var errMsg string
				if event.Data.Message != nil {
					errMsg = *event.Data.Message
				} else {
					errMsg = "unknown error"
				}
				eventChan <- streamErrMsg{err: fmt.Errorf("%s", errMsg)}
			case copilot.ToolExecutionStart:
				toolName := "unknown"
				if event.Data.ToolName != nil {
					toolName = *event.Data.ToolName
				}
				// Generate tool ID from timestamp
				toolID := fmt.Sprintf("tool-%d", time.Now().UnixNano())
				eventChan <- toolStartMsg{toolID: toolID, toolName: toolName}
			case copilot.ToolExecutionComplete:
				toolName := "unknown"
				if event.Data.ToolName != nil {
					toolName = *event.Data.ToolName
				}
				output := ""
				if event.Data.Result != nil {
					output = event.Data.Result.Content
				}
				// Assume success if we got a result (SDK doesn't expose error state directly)
				eventChan <- toolEndMsg{toolName: toolName, output: output, success: true}
			}
		})

		// Store unsubscribe for cleanup - send via message to update model
		eventChan <- unsubscribeMsg{fn: unsubscribe}

		// Send the message
		_, err := session.Send(copilot.MessageOptions{Prompt: userMessage})
		if err != nil {
			unsubscribe()
			return streamErrMsg{err: err}
		}

		return nil
	}
}

// Run starts the chat TUI.
func Run(
	ctx context.Context,
	session *copilot.Session,
	timeout time.Duration,
) error {
	model := New(session, timeout)
	model.ctx = ctx
	program := tea.NewProgram(
		model,
		tea.WithAltScreen(),
		tea.WithContext(ctx),
		tea.WithMouseCellMotion(), // Enable mouse support
	)

	_, err := program.Run()
	return err
}
