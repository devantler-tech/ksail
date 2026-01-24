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
	"github.com/muesli/reflow/wordwrap"
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

	// Permission dialog state
	permissionPending bool
	permissionDesc    string
	permissionRespCh  chan<- copilot.PermissionRequestResult

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

	case permissionRequestMsg:
		return m.handlePermissionRequest(msg)

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
	var inputContent string
	if m.permissionPending {
		// Show permission prompt with details
		var promptLines []string
		promptLines = append(promptLines, permissionTitleStyle.Render("⚠️  Permission Required"))
		if m.permissionDesc != "" {
			// Add description lines with styling
			for _, line := range strings.Split(m.permissionDesc, "\n") {
				if line != "" {
					promptLines = append(promptLines, "    "+permissionDescStyle.Render(line))
				}
			}
		}
		promptLines = append(promptLines, "")
		promptLines = append(
			promptLines,
			permissionDescStyle.Render("    Press [y] to allow, [n] to deny"),
		)
		promptText := strings.Join(promptLines, "\n")
		inputContent = permissionBoxStyle.Width(m.width - 2).Render(promptText)
	} else {
		inputContent = inputStyle.Width(m.width - 2).Render(m.textarea.View())
	}
	sections = append(sections, inputContent)

	// Footer/help
	var helpText string
	if m.permissionPending {
		helpText = "  Powered by GitHub Copilot"
	} else {
		helpText = "  ⏎ Send • ⌥⏎ Newline • ^L Clear • esc Quit • Powered by GitHub Copilot"
	}
	help := helpStyle.Render(helpText)
	sections = append(sections, help)

	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

// handleKeyMsg handles keyboard input.
func (m Model) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Handle permission dialog keys first
	if m.permissionPending {
		return m.handlePermissionKeys(msg)
	}

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
		// Clear chat history
		if !m.isStreaming {
			m.messages = make([]chatMessage, 0)
			m.currentResponse.Reset()
			m.updateViewportContent()
		}
	case "alt+enter":
		// Insert newline (alt+enter since terminals don't detect shift+enter)
		m.textarea.InsertString("\n")
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

// handlePermissionKeys handles keys during permission dialog.
func (m Model) handlePermissionKeys(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		m.permissionPending = false
		respChan := m.permissionRespCh
		m.permissionDesc = ""
		m.permissionRespCh = nil
		// Add approval message to chat
		m.messages = append(m.messages, chatMessage{
			role:    "tool",
			content: "✓ Permission granted",
		})
		m.updateViewportContent()
		// Send response async and resume waiting for events
		return m, tea.Batch(
			m.sendPermissionResponse(respChan, copilot.PermissionRequestResult{Kind: "approved"}),
			m.waitForEvent(),
		)
	case "n", "N":
		m.permissionPending = false
		respChan := m.permissionRespCh
		m.permissionDesc = ""
		m.permissionRespCh = nil
		// Add denial message to chat
		m.messages = append(m.messages, chatMessage{
			role:    "tool",
			content: "✗ Permission denied",
		})
		m.updateViewportContent()
		// Send response async and resume waiting for events
		return m, tea.Batch(
			m.sendPermissionResponse(respChan, copilot.PermissionRequestResult{
				Kind: "denied-interactively-by-user",
			}),
			m.waitForEvent(),
		)
	}
	// Ignore other keys during permission dialog
	return m, nil
}

// sendPermissionResponse returns a command that sends a permission response.
// This keeps the Update function non-blocking per Bubbletea best practices.
func (m *Model) sendPermissionResponse(
	respChan chan<- copilot.PermissionRequestResult,
	result copilot.PermissionRequestResult,
) tea.Cmd {
	return func() tea.Msg {
		if respChan != nil {
			respChan <- result
		}
		return nil
	}
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
	// Insert a tool message before the current assistant response
	if len(m.messages) > 0 && m.messages[len(m.messages)-1].role == "assistant" {
		lastAssistant := m.messages[len(m.messages)-1]
		m.messages = m.messages[:len(m.messages)-1]
		m.messages = append(m.messages, chatMessage{
			role:    "tool",
			content: fmt.Sprintf("🔧 Running: %s", msg.toolName),
		})
		m.messages = append(m.messages, lastAssistant)
	}
	m.updateViewportContent()
	return m, m.waitForEvent()
}

// handleToolEnd handles tool execution completion events.
func (m Model) handleToolEnd(msg toolEndMsg) (tea.Model, tea.Cmd) {
	// Update the last tool message to show completion
	for i := len(m.messages) - 1; i >= 0; i-- {
		if m.messages[i].role == "tool" &&
			strings.HasPrefix(m.messages[i].content, "🔧 Running:") {
			m.messages[i].content = strings.Replace(m.messages[i].content, "🔧 Running:", "✓", 1)
			break
		}
	}
	// Add output if available (truncate long output)
	if msg.output != "" {
		output := msg.output
		const maxOutputLines = 20
		lines := strings.Split(output, "\n")
		if len(lines) > maxOutputLines {
			lines = lines[:maxOutputLines]
			lines = append(lines, fmt.Sprintf("... (%d more lines)", len(lines)-maxOutputLines))
			output = strings.Join(lines, "\n")
		}
		m.messages = append(m.messages, chatMessage{
			role:    "tool-output",
			content: output,
		})
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

// handlePermissionRequest handles permission request events.
func (m Model) handlePermissionRequest(msg permissionRequestMsg) (tea.Model, tea.Cmd) {
	// Show permission dialog - store state and wait for user input
	m.permissionPending = true
	m.permissionDesc = formatPermissionDesc(msg.request)
	m.permissionRespCh = msg.respChan
	// Don't add to chat messages - permission details shown in bottom prompt only
	return m, m.waitForEvent()
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

	var builder strings.Builder
	for _, msg := range m.messages {
		switch msg.role {
		case "user":
			builder.WriteString(userMsgStyle.Render("▶ You"))
			builder.WriteString("\n")
			// Wrap user content
			wrapped := wordwrap.String(msg.content, wrapWidth)
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
			if msg.isStreaming {
				// Wrap streaming content
				wrapped := wordwrap.String(msg.content, wrapWidth)
				for _, line := range strings.Split(wrapped, "\n") {
					builder.WriteString("  ")
					builder.WriteString(line)
					builder.WriteString("\n")
				}
				builder.WriteString("  ▌")
			} else if msg.rendered != "" {
				builder.WriteString(msg.rendered)
			} else {
				wrapped := wordwrap.String(msg.content, wrapWidth)
				for _, line := range strings.Split(wrapped, "\n") {
					builder.WriteString("  ")
					builder.WriteString(line)
					builder.WriteString("\n")
				}
			}
			builder.WriteString("\n")
		case "tool":
			if strings.HasPrefix(msg.content, "✓") {
				builder.WriteString(toolSuccessStyle.Render("  " + msg.content))
			} else {
				builder.WriteString(toolMsgStyle.Render("  " + msg.content))
			}
			builder.WriteString("\n")
		case "tool-output":
			// Render tool output in a dimmed/code-like style
			wrapped := wordwrap.String(msg.content, wrapWidth-2)
			for _, line := range strings.Split(wrapped, "\n") {
				builder.WriteString(toolOutputStyle.Render("    " + line))
				builder.WriteString("\n")
			}
		}
	}
	m.viewport.SetContent(builder.String())
	m.viewport.GotoBottom()
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
				eventChan <- toolStartMsg{toolName: toolName}
			case copilot.ToolExecutionComplete:
				toolName := "unknown"
				if event.Data.ToolName != nil {
					toolName = *event.Data.ToolName
				}
				output := ""
				if event.Data.Result != nil {
					output = event.Data.Result.Content
				}
				eventChan <- toolEndMsg{toolName: toolName, output: output}
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
// The permissionBridge should be created before calling Run and its handler passed to the Copilot session.
func Run(
	ctx context.Context,
	session *copilot.Session,
	timeout time.Duration,
	permissionBridge *PermissionBridge,
) error {
	model := New(session, timeout)
	model.ctx = ctx
	program := tea.NewProgram(model, tea.WithAltScreen(), tea.WithContext(ctx))

	// Connect the permission bridge to the program for thread-safe messaging
	permissionBridge.SetProgram(program)

	_, err := program.Run()
	return err
}
