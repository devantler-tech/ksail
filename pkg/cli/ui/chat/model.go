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
	id           string
	name         string
	command      string // The actual command being executed (e.g., "ksail cluster list --all")
	status       toolStatus
	output       string
	expanded     bool // whether output is expanded in the view
	startTime    time.Time
	textPosition int // position in assistant response when tool was called
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

// extractCommandFromArgs extracts a command string from tool arguments for display.
// This helps users understand exactly what command is being executed.
func extractCommandFromArgs(toolName string, args any) string {
	argsMap, ok := args.(map[string]any)
	if !ok {
		return ""
	}

	switch toolName {
	case "ksail_cluster_list":
		cmd := "ksail cluster list"
		if all, ok := argsMap["all"].(bool); ok && all {
			cmd += " --all"
		}
		return cmd
	case "ksail_cluster_info":
		cmd := "ksail cluster info"
		if name, ok := argsMap["name"].(string); ok && name != "" {
			cmd += " --name " + name
		}
		return cmd
	case "ksail_workload_get":
		resource, _ := argsMap["resource"].(string)
		if resource == "" {
			return ""
		}
		cmd := "ksail workload get " + resource
		if name, ok := argsMap["name"].(string); ok && name != "" {
			cmd += " " + name
		}
		if ns, ok := argsMap["namespace"].(string); ok && ns != "" {
			cmd += " -n " + ns
		}
		if allNs, ok := argsMap["all_namespaces"].(bool); ok && allNs {
			cmd += " -A"
		}
		if output, ok := argsMap["output"].(string); ok && output != "" {
			cmd += " -o " + output
		}
		return cmd
	case "read_file":
		if path, ok := argsMap["path"].(string); ok && path != "" {
			return "cat " + path
		}
	case "list_directory":
		path := "."
		if p, ok := argsMap["path"].(string); ok && p != "" {
			path = p
		}
		return "ls " + path
	}
	return ""
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
	justCompleted   bool // true when a response just finished, shows "Ready" indicator
	sessionComplete bool // true when SessionIdle received - stops waiting for more events
	userScrolled    bool // true when user has scrolled away from bottom (pause auto-scroll)
	err             error
	quitting        bool
	ready           bool

	// Tool execution tracking
	tools            map[string]*toolExecution // keyed by tool ID
	toolOrder        []string                  // ordered list of tool IDs for rendering
	pendingToolCount int                       // count of tools started but not yet completed
	lastToolCall     string                    // last tool call signature for duplicate detection

	// Permission request state
	pendingPermission *permissionRequestMsg
	awaitingApproval  bool

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
func New(session *copilot.Session, timeout time.Duration) *Model {
	return NewWithEventChannel(session, timeout, nil)
}

// NewWithEventChannel creates a new chat TUI model with an optional pre-existing event channel.
// If eventChan is nil, a new channel is created. This allows external code to send events
// to the TUI (e.g., permission requests).
func NewWithEventChannel(
	session *copilot.Session,
	timeout time.Duration,
	eventChan chan tea.Msg,
) *Model {
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

	// Use provided event channel or create new one
	if eventChan == nil {
		eventChan = make(chan tea.Msg, 100)
	}

	return &Model{
		viewport:  viewPort,
		textarea:  textArea,
		spinner:   spin,
		renderer:  mdRenderer,
		messages:  make([]chatMessage, 0),
		session:   session,
		timeout:   timeout,
		ctx:       context.Background(),
		eventChan: eventChan,
		width:     defaultWidth,
		height:    defaultHeight,
		tools:     make(map[string]*toolExecution),
		toolOrder: make([]string, 0),
	}
}

// GetEventChannel returns the model's event channel for external use.
// This is useful for creating permission handlers that can send events to the TUI.
func (m *Model) GetEventChannel() chan tea.Msg {
	return m.eventChan
}

// Init initializes the model and returns an initial command.
func (m *Model) Init() tea.Cmd {
	return tea.Batch(textarea.Blink, m.spinner.Tick)
}

// Update handles messages and updates the model.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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

	case assistantMessageMsg:
		return m.handleAssistantMessage(msg)

	case toolStartMsg:
		return m.handleToolStart(msg)

	case toolEndMsg:
		return m.handleToolEnd(msg)

	case toolOutputChunkMsg:
		return m.handleToolOutputChunk(msg.toolID, msg.chunk)

	case ToolOutputChunkMsg:
		return m.handleToolOutputChunk(msg.ToolID, msg.Chunk)

	case streamEndMsg:
		return m.handleStreamEnd()

	case turnStartMsg:
		return m.handleTurnStart()

	case turnEndMsg:
		return m.handleTurnEnd()

	case reasoningMsg:
		return m.handleReasoning(msg)

	case abortMsg:
		return m.handleAbort()

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
		// Update viewport content if there are active tools or streaming to animate spinners
		if m.isStreaming || m.hasRunningTools() {
			m.updateViewportContent()
		}

	case tea.MouseMsg:
		return m.handleMouseMsg(msg)
	}

	// Update sub-components - allow input when awaiting approval
	if !m.isStreaming || m.awaitingApproval {
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
func (m *Model) View() string {
	if m.quitting {
		goodbye := statusStyle.Render("  Goodbye! Thanks for using KSail.\n")
		return logoStyle.Render(logo) + "\n\n" + goodbye
	}

	sections := make([]string, 0, 4)

	// Header with logo
	headerContent := logoStyle.Render(logo) + "\n" + taglineStyle.Render("  "+tagline)
	if m.isStreaming && !m.awaitingApproval {
		headerContent += "  " + m.spinner.View() + " " + statusStyle.Render("Thinking...")
	} else if m.justCompleted && !m.awaitingApproval {
		headerContent += "  " + statusStyle.Render("Ready ✓")
	}
	header := headerBoxStyle.Width(m.width - 2).Render(headerContent)
	sections = append(sections, header)

	// Chat viewport
	chatContent := viewportStyle.Width(m.width - 2).Render(m.viewport.View())
	sections = append(sections, chatContent)

	// Input area - show permission prompt if awaiting approval
	var inputContent string
	if m.awaitingApproval && m.pendingPermission != nil {
		// Build permission request display
		var permBox strings.Builder
		permBox.WriteString(warningStyle.Render("⚠ Permission Required") + "\n\n")

		// Show command (most important) if available - highlight it prominently
		if m.pendingPermission.command != "" {
			permBox.WriteString(toolMsgStyle.Render("  $ "+m.pendingPermission.command) + "\n")
		} else if m.pendingPermission.description != "" && m.pendingPermission.description != "shell" {
			// Show description if no command and it's not just "shell"
			permBox.WriteString(
				helpStyle.Render("  Operation: ") + m.pendingPermission.description + "\n",
			)
		} else {
			// Generic shell operation - show what we know
			permBox.WriteString(helpStyle.Render("  Tool: ") + m.pendingPermission.toolName + "\n")
			permBox.WriteString(helpStyle.Render("  Note: Shell command execution requested\n"))
			permBox.WriteString(
				helpStyle.Render("        (Command details not available from this tool)\n"),
			)
		}

		// Show arguments if present (separate from command)
		if len(m.pendingPermission.args) > 0 {
			permBox.WriteString(
				helpStyle.Render("  Args: ") + strings.Join(m.pendingPermission.args, " ") + "\n",
			)
		}

		// Show file path if present
		if m.pendingPermission.path != "" {
			permBox.WriteString(helpStyle.Render("  Path: ") + m.pendingPermission.path + "\n")
		}

		// Show content preview if present (for file write operations)
		if m.pendingPermission.content != "" {
			lines := strings.Split(m.pendingPermission.content, "\n")
			permBox.WriteString(helpStyle.Render("  Content:\n"))
			for _, line := range lines {
				permBox.WriteString(helpStyle.Render("    "+line) + "\n")
			}
		}

		permBox.WriteString("\n")
		permBox.WriteString(helpStyle.Render("  Press Y to approve, N to deny"))
		inputContent = inputStyle.Width(m.width - 2).Render(permBox.String())
	} else {
		inputContent = inputStyle.Width(m.width - 2).Render(m.textarea.View())
	}
	sections = append(sections, inputContent)

	// Footer/help
	var helpText string
	if m.awaitingApproval {
		helpText = "  Y Approve • N Deny • esc Cancel • Powered by GitHub Copilot"
	} else if len(m.toolOrder) > 0 {
		helpText = "  ⏎ Send • ⌥⏎ Newline • ⇥ Toggle Output • ^L Clear • esc Quit • Powered by GitHub Copilot"
	} else {
		helpText = "  ⏎ Send • ⌥⏎ Newline • ^L Clear • ^T Toggle tools • esc Quit"
	}
	help := helpStyle.Render(helpText)
	sections = append(sections, help)

	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

// handleKeyMsg handles keyboard input.
func (m *Model) handleKeyMsg(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Handle permission approval mode separately
	if m.awaitingApproval {
		return m.handleApprovalKey(msg)
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
	case "ctrl+t":
		// Toggle ALL tool outputs expanded/collapsed
		if len(m.toolOrder) > 0 {
			// Determine target state by checking first completed tool
			expandAll := false
			for _, id := range m.toolOrder {
				if tool := m.tools[id]; tool != nil && tool.status != toolRunning {
					expandAll = !tool.expanded
					break
				}
			}
			// Apply to all completed tools
			for _, id := range m.toolOrder {
				if tool := m.tools[id]; tool != nil && tool.status != toolRunning {
					tool.expanded = expandAll
				}
			}
			m.updateViewportContent()
		}
		return m, nil
	case "enter":
		// Send message on Enter
		if !m.isStreaming && strings.TrimSpace(m.textarea.Value()) != "" {
			content := m.textarea.Value()
			m.textarea.Reset()
			// Set streaming immediately so spinner shows right away
			m.isStreaming = true
			m.justCompleted = false // Clear completion indicator
			// Batch spinner tick with send command to start animation immediately
			return m, tea.Batch(m.spinner.Tick, m.sendMessageCmd(content))
		}
	}

	// Update textarea for other keys
	var taCmd tea.Cmd
	m.textarea, taCmd = m.textarea.Update(msg)
	// Clear completion indicator when user starts typing
	if m.justCompleted {
		m.justCompleted = false
	}
	return m, taCmd
}

// handleApprovalKey handles keyboard input when awaiting permission approval.
func (m *Model) handleApprovalKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		m.cleanup()
		m.quitting = true
		return m, tea.Quit
	case "y", "Y":
		// Approve the permission request
		if m.pendingPermission != nil && m.pendingPermission.respondChan != nil {
			m.pendingPermission.respondChan <- true
		}
		m.awaitingApproval = false
		m.pendingPermission = nil
		m.textarea.Reset()
		m.textarea.Placeholder = "Ask me anything about Kubernetes, KSail, or cluster management..."
		m.updateViewportContent()
		return m, m.waitForEvent()
	case "n", "N", "esc":
		// Deny the permission request
		if m.pendingPermission != nil && m.pendingPermission.respondChan != nil {
			m.pendingPermission.respondChan <- false
		}
		m.awaitingApproval = false
		m.pendingPermission = nil
		m.textarea.Reset()
		m.textarea.Placeholder = "Ask me anything about Kubernetes, KSail, or cluster management..."
		m.updateViewportContent()
		return m, m.waitForEvent()
	}
	return m, nil
}

// handleUserSubmit handles user message submission.
func (m *Model) handleUserSubmit(msg userSubmitMsg) (tea.Model, tea.Cmd) {
	// Clean up any previous subscription before starting a new one
	if m.unsubscribe != nil {
		m.unsubscribe()
		m.unsubscribe = nil
	}

	// Drain any stale events from previous conversation turn
	// This prevents old events from interfering with the new message
	m.drainEventChannel()

	// Clear tool state for new conversation turn
	// Keep tool history visible but reset running state tracking
	m.tools = make(map[string]*toolExecution)
	m.toolOrder = make([]string, 0)
	m.pendingToolCount = 0   // Reset pending tool count
	m.sessionComplete = false // Reset session complete flag
	m.lastToolCall = ""       // Reset duplicate detection

	m.messages = append(m.messages, chatMessage{
		role:    "user",
		content: msg.content,
	})
	m.isStreaming = true
	m.justCompleted = false // Clear completion indicator
	m.userScrolled = false  // Resume auto-scroll on new message
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

// drainEventChannel removes any stale events from the channel.
// This prevents events from a previous conversation turn from interfering
// with a new message submission.
func (m *Model) drainEventChannel() {
	for {
		select {
		case <-m.eventChan:
			// Discard stale event
		default:
			// Channel is empty
			return
		}
	}
}

// handleStreamChunk handles streaming response chunks.
func (m *Model) handleStreamChunk(msg streamChunkMsg) (tea.Model, tea.Cmd) {
	if msg.content != "" {
		m.currentResponse.WriteString(msg.content)
		if len(m.messages) > 0 {
			m.messages[len(m.messages)-1].content = m.currentResponse.String()
		}
		m.updateViewportContent()
	}
	return m, m.waitForEvent()
}

// handleAssistantMessage handles the final complete message from the assistant.
// Per SDK best practices, this event is always sent (regardless of streaming)
// and contains the complete response. It's more reliable for completion than
// accumulating deltas.
func (m *Model) handleAssistantMessage(msg assistantMessageMsg) (tea.Model, tea.Cmd) {
	// If we have more complete content from the final message, use it
	// This handles cases where deltas might have been missed
	if len(msg.content) > m.currentResponse.Len() {
		m.currentResponse.Reset()
		m.currentResponse.WriteString(msg.content)
	}

	// Update the message content
	if len(m.messages) > 0 {
		last := &m.messages[len(m.messages)-1]
		if last.role == "assistant" {
			last.content = m.currentResponse.String()
			// Don't render yet - wait for SessionIdle or TurnEnd for proper completion
		}
	}

	m.updateViewportContent()
	return m, m.waitForEvent()
}

// handleToolStart handles tool execution start events.
func (m *Model) handleToolStart(msg toolStartMsg) (tea.Model, tea.Cmd) {
	// Increment pending tool count for reliable completion tracking
	m.pendingToolCount++

	// Generate tool ID if not provided
	toolID := msg.toolID
	if toolID == "" {
		toolID = fmt.Sprintf("tool-%d", time.Now().UnixNano())
	}

	// Record current position in assistant response for interleaving
	textPos := m.currentResponse.Len()

	// Create tool execution entry
	tool := &toolExecution{
		id:           toolID,
		name:         msg.toolName,
		command:      msg.command,
		status:       toolRunning,
		expanded:     true, // expanded by default while running
		startTime:    time.Now(),
		textPosition: textPos,
	}
	m.tools[toolID] = tool
	m.toolOrder = append(m.toolOrder, toolID)

	// DON'T insert tool as separate message - render inline with assistant response
	m.updateViewportContent()
	return m, m.waitForEvent()
}

// handleToolEnd handles tool execution completion events.
func (m *Model) handleToolEnd(msg toolEndMsg) (tea.Model, tea.Cmd) {
	// Find the tool to complete. The SDK's ToolExecutionComplete event often
	// doesn't include the tool name, so we use FIFO matching as primary strategy.
	var tool *toolExecution

	// Strategy 1: Try matching by tool ID if provided
	if msg.toolID != "" {
		tool = m.tools[msg.toolID]
	}

	// Strategy 2: Try matching by name if provided and not "unknown"
	if tool == nil && msg.toolName != "" && msg.toolName != "unknown" {
		for _, id := range m.toolOrder {
			t := m.tools[id]
			if t != nil && t.name == msg.toolName && t.status == toolRunning {
				tool = t
				break
			}
		}
	}

	// Strategy 3: FIFO - match the first running tool (SDK doesn't always provide name)
	if tool == nil {
		for _, id := range m.toolOrder {
			t := m.tools[id]
			if t != nil && t.status == toolRunning {
				tool = t
				break
			}
		}
	}

	// Decrement pending tool count regardless of whether we found the tool
	// This ensures our count stays in sync with SDK events
	if m.pendingToolCount > 0 {
		m.pendingToolCount--
	}

	if tool != nil {
		// Update tool status
		if msg.success {
			tool.status = toolSuccess
		} else {
			tool.status = toolFailed
		}
		// Only use SDK output if we didn't stream any output already
		if tool.output == "" && msg.output != "" {
			tool.output = msg.output
		}
		// Keep expanded so users can follow along with output (press Tab to collapse)
		tool.expanded = true
	}

	m.updateViewportContent()

	// Always keep waiting for events after tool completion.
	// The SDK will fire another turn for the assistant to process results and respond.
	// Only AssistantTurnEnd (with response content) or SessionIdle should trigger cleanup.
	return m, m.waitForEvent()
}

// handleToolOutputChunk handles real-time output chunks from running tools.
func (m *Model) handleToolOutputChunk(toolID, chunk string) (tea.Model, tea.Cmd) {
	// The toolID from generator is actually the tool name (e.g., "ksail_cluster_list")
	// Find the FIRST running tool that matches this name (FIFO order)
	var tool *toolExecution
	for _, id := range m.toolOrder {
		t := m.tools[id]
		if t != nil && t.name == toolID && t.status == toolRunning {
			tool = t
			break
		}
	}

	if tool != nil {
		// Append the chunk to the tool's output
		tool.output += chunk
		m.updateViewportContent()
	}

	// Always keep waiting - more output chunks or events (like turn completion) may come
	return m, m.waitForEvent()
}

// handleStreamEnd handles stream completion events (SessionIdle).
// SessionIdle means the session has finished processing. However, if tools are
// still pending, we keep waiting for their completion.
func (m *Model) handleStreamEnd() (tea.Model, tea.Cmd) {
	// If there are pending tools, keep waiting - don't mark as complete yet
	if m.pendingToolCount > 0 || m.hasRunningTools() {
		return m, m.waitForEvent()
	}

	// Truly complete - mark session as finished
	m.isStreaming = false
	m.justCompleted = true  // Show "Ready" indicator
	m.sessionComplete = true // Stop waiting for more events
	if len(m.messages) > 0 {
		last := &m.messages[len(m.messages)-1]
		last.isStreaming = false
		last.content = m.currentResponse.String()
		// Render markdown using cached renderer (avoids terminal queries)
		last.rendered = renderMarkdownWithRenderer(m.renderer, last.content)
	}
	m.updateViewportContent()
	m.cleanup() // Unsubscribe from events - session is complete
	return m, nil
}

// handleTurnEnd handles assistant turn end events (AssistantTurnEnd).
// AssistantTurnEnd fires after each turn, including intermediate turns where the
// assistant calls tools. We only mark as complete if there's content and no pending tools.
func (m *Model) handleTurnEnd() (tea.Model, tea.Cmd) {
	// If there are pending tools, keep waiting - more turns are coming
	if m.pendingToolCount > 0 || m.hasRunningTools() {
		return m, m.waitForEvent()
	}

	// If the assistant hasn't produced any response content yet, keep waiting
	// This handles the case where the assistant only called tools on this turn
	// and needs another turn to process the results and respond.
	if m.currentResponse.Len() == 0 {
		return m, m.waitForEvent()
	}

	// Assistant has produced content and no tools are pending - we're done
	m.isStreaming = false
	m.justCompleted = true
	m.sessionComplete = true // Mark session as complete
	if len(m.messages) > 0 {
		last := &m.messages[len(m.messages)-1]
		last.isStreaming = false
		last.content = m.currentResponse.String()
		last.rendered = renderMarkdownWithRenderer(m.renderer, last.content)
	}
	m.updateViewportContent()
	// Keep waiting for SessionIdle to confirm completion
	return m, m.waitForEvent()
}

// handleTurnStart handles assistant turn start events (AssistantTurnStart).
// This fires when the assistant begins a new turn, ensuring we're in streaming mode.
func (m *Model) handleTurnStart() (tea.Model, tea.Cmd) {
	// Ensure we're in streaming mode when a new turn starts
	m.isStreaming = true
	m.justCompleted = false
	m.sessionComplete = false // New turn means session is active again
	m.updateViewportContent()
	return m, m.waitForEvent()
}

// handleReasoning handles reasoning events from the assistant.
// These indicate the LLM is actively "thinking" about the response.
func (m *Model) handleReasoning(msg reasoningMsg) (tea.Model, tea.Cmd) {
	// Reasoning events confirm the LLM is actively working
	// We just keep streaming state active and wait for more events
	m.isStreaming = true
	m.justCompleted = false
	// Optionally, we could append reasoning content to a separate buffer
	// For now, just acknowledge we're still processing
	m.updateViewportContent()
	return m, m.waitForEvent()
}

// handleAbort handles session abort events.
func (m *Model) handleAbort() (tea.Model, tea.Cmd) {
	m.isStreaming = false
	m.cleanup()
	if len(m.messages) > 0 && m.messages[len(m.messages)-1].role == "assistant" {
		m.messages[len(m.messages)-1].content += "\n\n[Session aborted]"
		m.messages[len(m.messages)-1].isStreaming = false
	}
	m.updateViewportContent()
	return m, nil
}

// handleStreamErr handles streaming error events.
func (m *Model) handleStreamErr(msg streamErrMsg) (tea.Model, tea.Cmd) {
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

// handlePermissionRequest handles permission request events from the Copilot SDK.
func (m *Model) handlePermissionRequest(msg permissionRequestMsg) (tea.Model, tea.Cmd) {
	m.pendingPermission = &msg
	m.awaitingApproval = true

	// Update textarea to show approval prompt
	m.textarea.Reset()
	m.textarea.Placeholder = "Press Y to approve, N to deny"
	m.textarea.Focus()

	m.updateViewportContent()
	return m, nil
}

// handleMouseMsg handles mouse events.
func (m *Model) handleMouseMsg(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	// Track scroll position before update
	wasAtBottom := m.viewport.AtBottom()

	// Pass mouse events to viewport for scrolling
	var vpCmd tea.Cmd
	m.viewport, vpCmd = m.viewport.Update(msg)

	// Detect user scrolling away from bottom (pause auto-scroll)
	if wasAtBottom && !m.viewport.AtBottom() {
		m.userScrolled = true
	}
	// Resume auto-scroll if user scrolls back to bottom
	if m.viewport.AtBottom() {
		m.userScrolled = false
	}

	return m, vpCmd
}

// waitForEvent returns a command that waits for an event from the channel.
// Uses select to allow context cancellation.
func (m *Model) waitForEvent() tea.Cmd {
	ctx := m.ctx
	eventChan := m.eventChan
	timeout := m.timeout
	return func() tea.Msg {
		// Use timeout to detect stuck conditions (e.g., assistant looping on tools)
		timer := time.NewTimer(timeout)
		defer timer.Stop()

		select {
		case msg := <-eventChan:
			return msg
		case <-timer.C:
			return streamErrMsg{err: fmt.Errorf("response timed out after %v - the assistant may be stuck", timeout)}
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
	// Don't drain the channel - tool completion events may still be pending
	// and need to be processed to update tool status indicators
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

	oldWidth := m.viewport.Width
	m.viewport.Width = contentWidth - 2
	m.viewport.Height = viewportHeight
	m.textarea.SetWidth(contentWidth - 2)

	// If viewport width changed, recreate the renderer and re-render completed messages
	if oldWidth != m.viewport.Width {
		m.renderer = createRenderer(m.viewport.Width - 4)
		m.reRenderCompletedMessages()
		m.updateViewportContent()
	}
}

// reRenderCompletedMessages re-renders all completed assistant messages with the current renderer.
func (m *Model) reRenderCompletedMessages() {
	for i := range m.messages {
		msg := &m.messages[i]
		if msg.role == "assistant" && msg.content != "" {
			msg.rendered = renderMarkdownWithRenderer(m.renderer, msg.content)
		}
	}
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

			// Interleave tools with assistant text based on their textPosition
			content := msg.content
			lastPos := 0

			// Render tools at their positions, interleaved with text
			for toolIdx < len(m.toolOrder) {
				tool := m.tools[m.toolOrder[toolIdx]]
				if tool == nil {
					toolIdx++
					continue
				}

				// Render text before this tool's position
				if tool.textPosition > lastPos && tool.textPosition <= len(content) {
					textBefore := content[lastPos:tool.textPosition]
					if textBefore != "" {
						wrapped := wordwrap.WrapString(textBefore, wrapWidthUint)
						for _, line := range strings.Split(wrapped, "\n") {
							builder.WriteString("  ")
							builder.WriteString(line)
							builder.WriteString("\n")
						}
					}
					lastPos = tool.textPosition
				}

				// Render the tool
				m.renderToolInline(&builder, tool, wrapWidthUint)
				toolIdx++
			}

			// Render remaining text after all tools
			if lastPos < len(content) {
				remainingText := content[lastPos:]
				if remainingText != "" {
					if msg.isStreaming {
						wrapped := wordwrap.WrapString(remainingText, wrapWidthUint)
						for _, line := range strings.Split(wrapped, "\n") {
							builder.WriteString("  ")
							builder.WriteString(line)
							builder.WriteString("\n")
						}
					} else if msg.rendered != "" && lastPos == 0 {
						// Use rendered markdown if no tools were interleaved
						builder.WriteString(msg.rendered)
					} else {
						wrapped := wordwrap.WrapString(remainingText, wrapWidthUint)
						for _, line := range strings.Split(wrapped, "\n") {
							builder.WriteString("  ")
							builder.WriteString(line)
							builder.WriteString("\n")
						}
					}
				}
			} else if lastPos == 0 && !msg.isStreaming && msg.rendered != "" {
				// No tools, use pre-rendered markdown
				builder.WriteString(msg.rendered)
			}

			// Show streaming cursor
			if msg.isStreaming {
				builder.WriteString("  ▌")
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
	// Only auto-scroll if user hasn't manually scrolled up
	if !m.userScrolled {
		m.viewport.GotoBottom()
	}
}

// renderToolInline renders a tool execution inline within an assistant response.
func (m *Model) renderToolInline(builder *strings.Builder, tool *toolExecution, wrapWidth uint) {
	humanName := humanizeToolName(tool.name)

	// Determine what to display: command if available, otherwise humanized name
	displayName := humanName
	if tool.command != "" {
		displayName = "$ " + tool.command
	}

	switch tool.status {
	case toolRunning:
		// Running: show spinner with command/action
		line := fmt.Sprintf("  %s %s", m.spinner.View(), displayName)
		builder.WriteString(toolMsgStyle.Render(line))
		builder.WriteString("\n")
		// Show streaming output while running (always expanded during execution)
		if tool.output != "" {
			m.renderToolOutput(builder, tool.output, wrapWidth)
		}

	case toolSuccess:
		// Completed: show checkmark with summary
		summary := m.getToolSummary(tool)
		if tool.expanded {
			line := fmt.Sprintf("  ✓ %s", displayName)
			builder.WriteString(toolCollapsedStyle.Render(line))
			builder.WriteString("\n")
			// Show output when expanded
			if tool.output != "" {
				m.renderToolOutput(builder, tool.output, wrapWidth)
			}
		} else {
			// Collapsed: show name + brief summary on same line
			line := fmt.Sprintf("  ✓ %s", displayName)
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
			line := fmt.Sprintf("  ✗ %s", displayName)
			builder.WriteString(errorStyle.Render(line))
			builder.WriteString("\n")
			if tool.output != "" {
				m.renderToolOutput(builder, tool.output, wrapWidth)
			}
		} else {
			line := fmt.Sprintf("  ✗ %s", displayName)
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
	const maxOutputLines = 10 // Show 10 lines by default, press Tab to expand for full output
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
			case copilot.AssistantTurnStart:
				// New turn started - reset streaming state
				eventChan <- turnStartMsg{}
			case copilot.AssistantMessageDelta:
				// Streaming message chunk - incremental text
				if event.Data.DeltaContent != nil {
					eventChan <- streamChunkMsg{content: *event.Data.DeltaContent}
				}
			case copilot.AssistantMessage:
				// Final complete message - SDK always sends this regardless of streaming
				// This is more reliable than tracking deltas for completion detection
				if event.Data.Content != nil {
					eventChan <- assistantMessageMsg{content: *event.Data.Content}
				}
			case copilot.AssistantReasoning, copilot.AssistantReasoningDelta:
				// Reasoning events - LLM is "thinking"
				// We track these to know the LLM is actively processing
				var content string
				if event.Data.Content != nil {
					content = *event.Data.Content
				} else if event.Data.DeltaContent != nil {
					content = *event.Data.DeltaContent
				}
				eventChan <- reasoningMsg{
					content: content,
					isDelta: event.Type == copilot.AssistantReasoningDelta,
				}
			case copilot.SessionIdle:
				// SessionIdle means the session is truly idle - all turns complete
				eventChan <- streamEndMsg{}
			case copilot.AssistantTurnEnd:
				// AssistantTurnEnd fires after each turn, including intermediate turns
				// where the assistant calls tools. We use a "soft end" signal that
				// only completes if the assistant has produced a response.
				eventChan <- turnEndMsg{}
			case copilot.Abort:
				// Session aborted - signal error and stop
				eventChan <- abortMsg{}
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
				// Extract command from arguments if available (for ksail tools)
				command := extractCommandFromArgs(toolName, event.Data.Arguments)
				// Generate tool ID from timestamp
				toolID := fmt.Sprintf("tool-%d", time.Now().UnixNano())
				eventChan <- toolStartMsg{toolID: toolID, toolName: toolName, command: command}
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

// Run starts the chat TUI and returns a permission handler for integration with the Copilot SDK.
// The returned handler can be used with SessionConfig.OnPermissionRequest to enable interactive
// permission prompting within the TUI.
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

// RunWithEventChannel starts the chat TUI with a pre-created event channel.
// This allows external code (like permission handlers) to send events to the TUI.
func RunWithEventChannel(
	ctx context.Context,
	session *copilot.Session,
	timeout time.Duration,
	eventChan chan tea.Msg,
) error {
	model := NewWithEventChannel(session, timeout, eventChan)
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

// createTextarea creates and configures the input textarea.
func createTextarea() textarea.Model {
	ta := textarea.New()
	ta.Placeholder = "Ask me anything about Kubernetes, KSail, or cluster management..."
	ta.Focus()
	ta.CharLimit = 4096
	ta.SetWidth(defaultWidth - 6)
	ta.SetHeight(inputHeight)
	ta.ShowLineNumbers = false
	ta.SetPromptFunc(2, func(lineIdx int) string {
		if lineIdx == 0 {
			return "> "
		}
		return "  "
	})
	ta.FocusedStyle.CursorLine = lipgloss.NewStyle()
	ta.FocusedStyle.Placeholder = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	return ta
}

// createSpinner creates and configures the loading spinner.
func createSpinner() spinner.Model {
	s := spinner.New()
	s.Spinner = spinner.MiniDot
	s.Style = spinnerStyle
	return s
}

// hasRunningTools returns true if any tools are currently running.
func (m *Model) hasRunningTools() bool {
	for _, tool := range m.tools {
		if tool != nil && tool.status == toolRunning {
			return true
		}
	}
	return false
}

// CreateTUIPermissionHandler creates a permission handler that integrates with the TUI.
// It sends permission requests to the provided event channel and waits for a response.
// This allows the TUI to display permission prompts and collect user input.
func CreateTUIPermissionHandler(eventChan chan<- tea.Msg) copilot.PermissionHandler {
	return func(
		request copilot.PermissionRequest,
		_ copilot.PermissionInvocation,
	) (copilot.PermissionRequestResult, error) {
		// Extract tool name from request.Extra
		toolName := ""
		if request.Extra != nil {
			if name, ok := request.Extra["toolName"].(string); ok {
				toolName = name
			}
		}
		if toolName == "" {
			toolName = request.Kind
		}

		// Humanize the tool name for display
		humanName := humanizeToolName(toolName)

		// Extract command from request.Extra if available
		command := ""
		if request.Extra != nil {
			if cmd, ok := request.Extra["command"].(string); ok {
				command = cmd
			}
			// Also check for 'text' field which might contain command details
			if command == "" {
				if text, ok := request.Extra["text"].(string); ok {
					command = text
				}
			}
		}

		// Extract arguments
		var args []string
		if request.Extra != nil {
			if argsAny, ok := request.Extra["args"].([]any); ok {
				for _, arg := range argsAny {
					args = append(args, fmt.Sprintf("%v", arg))
				}
			}
		}

		// Extract file path
		path := ""
		if request.Extra != nil {
			if p, ok := request.Extra["path"].(string); ok {
				path = p
			}
		}

		// Extract content (truncate for display)
		content := ""
		if request.Extra != nil {
			if c, ok := request.Extra["content"].(string); ok {
				content = c
				if len(content) > 200 {
					content = content[:200] + "..."
				}
			}
		}

		// Build description from Kind
		description := request.Kind

		// Create response channel
		responseChan := make(chan bool, 1)

		// Send permission request to TUI - use humanized name for display
		eventChan <- permissionRequestMsg{
			toolName:    humanName,
			command:     command,
			args:        args,
			path:        path,
			content:     content,
			description: description,
			respondChan: responseChan,
		}

		// Wait for response from TUI
		approved := <-responseChan

		if approved {
			return copilot.PermissionRequestResult{Kind: "approved"}, nil
		}
		return copilot.PermissionRequestResult{Kind: "denied-interactively-by-user"}, nil
	}
}
