package chat

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	copilot "github.com/github/copilot-sdk/go"
)

const (
	defaultWidth  = 100
	defaultHeight = 30
	inputHeight   = 3
	headerHeight  = logoHeight + 3 // logo + tagline + border
	footerHeight  = 1              // single line help text

	// Shared picker/output constants.
	maxPickerVisible   = 3  // maximum visible items in picker modals
	maxToolOutputLines = 10 // maximum lines shown in collapsed tool output
	minWrapWidth       = 20 // minimum width for text wrapping
)

// chatMessage represents a single message in the chat history.
type chatMessage struct {
	role        string // "user", "assistant", or "tool"
	content     string
	rendered    string // markdown-rendered content for assistant messages
	isStreaming bool
	tools       []*toolExecution // tools executed during this assistant message
	toolOrder   []string         // ordered tool IDs for this message
}

// permissionResponse records a user's response to a permission request.
type permissionResponse struct {
	toolName string
	command  string
	allowed  bool
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
	userScrolled    bool // true when user has scrolled away from bottom (pause auto-scroll)
	err             error
	quitting        bool
	ready           bool

	// Prompt history
	history      []string // previously submitted prompts
	historyIndex int      // -1 means not browsing history, 0+ is position in history
	savedInput   string   // saves current input when browsing history

	// Tool execution tracking
	tools            map[string]*toolExecution // keyed by tool ID
	toolOrder        []string                  // ordered list of tool IDs for rendering
	pendingToolCount int                       // number of tools awaiting completion
	lastToolCall     time.Time                 // timestamp of most recent tool start

	// Session completion tracking
	sessionComplete bool       // true when SessionIdle has been received
	unsubscribe     func()     // function to unsubscribe from session events
	unsubscribeMu   sync.Mutex // protects unsubscribe access

	// Dimensions
	width  int
	height int

	// Copilot session and model switching
	session       *copilot.Session
	client        *copilot.Client
	sessionConfig *copilot.SessionConfig
	timeout       time.Duration
	ctx           context.Context

	// Model selection
	currentModel     string              // currently selected model ID
	availableModels  []copilot.ModelInfo // models the user has access to
	showModelPicker  bool                // true when model picker overlay is visible
	modelPickerIndex int                 // currently highlighted model in picker

	// Permission request handling
	pendingPermission *permissionRequestMsg // current permission request awaiting user response
	permissionHistory []permissionResponse  // history of permission decisions

	// Session management
	currentSessionID     string            // ID of the current session (empty if new)
	availableSessions    []SessionMetadata // cached list of available sessions
	showSessionPicker    bool              // true when session picker overlay is visible
	sessionPickerIndex   int               // currently highlighted session in picker
	confirmDeleteSession bool              // true when confirming session deletion
	renamingSession      bool              // true when renaming a session
	sessionRenameInput   string            // current rename input text

	// Markdown renderer (cached to avoid terminal queries)
	renderer *glamour.TermRenderer

	// Mode selection (agent executes tools, plan describes only)
	agentMode bool // true = agent (execute), false = plan (describe only)

	// Channel for async streaming events from Copilot
	eventChan chan tea.Msg
}

// New creates a new chat TUI model.
func New(
	session *copilot.Session,
	client *copilot.Client,
	sessionConfig *copilot.SessionConfig,
	models []copilot.ModelInfo,
	currentModel string,
	timeout time.Duration,
) *Model {
	return NewWithEventChannel(session, client, sessionConfig, models, currentModel, timeout, nil)
}

// NewWithEventChannel creates a new chat TUI model with an optional pre-existing event channel.
// If eventChan is nil, a new channel is created. This allows external code to send events
// to the TUI (e.g., permission requests).
func NewWithEventChannel(
	session *copilot.Session,
	client *copilot.Client,
	sessionConfig *copilot.SessionConfig,
	models []copilot.ModelInfo,
	currentModel string,
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
		viewport:         viewPort,
		textarea:         textArea,
		spinner:          spin,
		renderer:         mdRenderer,
		messages:         make([]chatMessage, 0),
		session:          session,
		client:           client,
		sessionConfig:    sessionConfig,
		currentSessionID: session.SessionID, // Track the SDK's session ID
		timeout:          timeout,
		ctx:              context.Background(),
		eventChan:        eventChan,
		width:            defaultWidth,
		height:           defaultHeight,
		tools:            make(map[string]*toolExecution),
		toolOrder:        make([]string, 0),
		history:          make([]string, 0),
		historyIndex:     -1,
		availableModels:  models,
		currentModel:     currentModel,
		agentMode:        true, // Default to agent mode
	}
}

// GetEventChannel returns the model's event channel for external use.
// This is useful for creating permission handlers that can send events to the TUI.
func (m *Model) GetEventChannel() chan tea.Msg {
	return m.eventChan
}

// Init initializes the model and returns an initial command.
func (m *Model) Init() tea.Cmd {
	// Do not auto-load or replace the existing session here.
	// Session loading should be driven explicitly by the caller or user actions.
	return tea.Batch(textarea.Blink, m.spinner.Tick)
}

// Update handles messages and updates the model.
func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKeyMsg(msg)

	case tea.MouseMsg:
		// Handle only mouse wheel scrolling - let click/drag pass through for text selection
		if msg.Button == tea.MouseButtonWheelUp {
			m.viewport.ScrollUp(3)
			m.userScrolled = !m.viewport.AtBottom()
			return m, nil
		}
		if msg.Button == tea.MouseButtonWheelDown {
			m.viewport.ScrollDown(3)
			if m.viewport.AtBottom() {
				m.userScrolled = false
			}
			return m, nil
		}
		// Ignore other mouse events (clicks, drags) - terminal handles text selection
		return m, nil

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

	case permissionRequestMsg:
		return m.handlePermissionRequest(&msg)

	case PermissionRequestMsg:
		return m.handlePermissionRequest(&permissionRequestMsg{
			toolCallID: msg.ToolCallID,
			toolName:   msg.ToolName,
			command:    msg.Command,
			arguments:  msg.Arguments,
			response:   msg.Response,
		})

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

	case spinner.TickMsg:
		// Always update spinner to keep it ticking
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		cmds = append(cmds, cmd)
		// Update viewport content if there are active tools or streaming to animate spinners
		if m.isStreaming || m.hasRunningTools() {
			m.updateViewportContent()
		}
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
func (m *Model) View() string {
	if m.quitting {
		goodbye := statusStyle.Render("  Goodbye! Thanks for using KSail.\n")
		return logoStyle.Render(logo()) + "\n\n" + goodbye
	}

	sections := make([]string, 0, 4)

	// Header, chat viewport, input/modal, and footer
	sections = append(sections, m.renderHeader())
	sections = append(sections, viewportStyle.Width(m.width-2).Render(m.viewport.View()))
	sections = append(sections, m.renderInputOrModal())
	sections = append(sections, m.renderFooter())

	// Join sections and clip final output to terminal width to prevent any wrapping
	output := lipgloss.JoinVertical(lipgloss.Left, sections...)
	return lipgloss.NewStyle().MaxWidth(m.width).Render(output)
}

// renderHeader renders the header section with logo and status.
func (m *Model) renderHeader() string {
	headerContentWidth := max(m.width-6, 1)

	// Truncate each logo line by display width (handles Unicode properly)
	logoLines := strings.Split(logo(), "\n")
	truncateStyle := lipgloss.NewStyle().MaxWidth(headerContentWidth).Inline(true)
	var clippedLogo strings.Builder
	for i, line := range logoLines {
		clippedLine := truncateStyle.Render(line)
		clippedLogo.WriteString(clippedLine)
		if i < len(logoLines)-1 {
			clippedLogo.WriteString("\n")
		}
	}
	logoRendered := logoStyle.Render(clippedLogo.String())

	// Build tagline with right-aligned status
	taglineRow := m.buildTaglineRow(headerContentWidth)
	taglineRow = lipgloss.NewStyle().MaxWidth(headerContentWidth).Inline(true).Render(taglineRow)

	headerContent := logoRendered + "\n" + taglineRow
	return headerBoxStyle.Width(m.width - 2).Render(headerContent)
}

// buildTaglineRow builds the tagline row with right-aligned status indicator.
func (m *Model) buildTaglineRow(contentWidth int) string {
	taglineText := taglineStyle.Render("  " + tagline())
	statusText := m.buildStatusText()

	if statusText == "" {
		return taglineText
	}

	taglineLen := lipgloss.Width(taglineText)
	statusLen := lipgloss.Width(statusText)
	spacing := max(contentWidth-taglineLen-statusLen, 2)
	return taglineText + strings.Repeat(" ", spacing) + statusText
}

// buildStatusText builds the status indicator text (mode, model, streaming state).
func (m *Model) buildStatusText() string {
	var statusParts []string

	// Mode icon: </> for Agent, ≡ for Plan
	modeStyle := lipgloss.NewStyle().Foreground(lipgloss.ANSIColor(14))
	if m.agentMode {
		statusParts = append(statusParts, modeStyle.Render("</>"))
	} else {
		statusParts = append(statusParts, modeStyle.Render("≡"))
	}

	// Model name
	modelStyle := lipgloss.NewStyle().Foreground(lipgloss.ANSIColor(8))
	if m.currentModel != "" {
		statusParts = append(statusParts, modelStyle.Render(m.currentModel))
	} else {
		statusParts = append(statusParts, modelStyle.Render("auto"))
	}

	// Streaming state
	if m.isStreaming {
		statusParts = append(statusParts, m.spinner.View()+" "+statusStyle.Render("Thinking..."))
	} else if m.justCompleted {
		statusParts = append(statusParts, statusStyle.Render("Ready ✓"))
	}

	return strings.Join(statusParts, " • ")
}

// renderInputOrModal renders either the input area or active modal.
func (m *Model) renderInputOrModal() string {
	if m.pendingPermission != nil {
		return m.renderPermissionModal()
	}
	if m.showModelPicker {
		return m.renderModelPickerModal()
	}
	if m.showSessionPicker {
		return m.renderSessionPickerModal()
	}
	return inputStyle.Width(m.width - 2).Render(m.textarea.View())
}

// renderFooter renders the context-aware help text footer.
func (m *Model) renderFooter() string {
	helpText := m.getContextualHelpText()
	return lipgloss.NewStyle().MaxWidth(m.width).Inline(true).Render(helpStyle.Render(helpText))
}

// handleUserSubmit handles user message submission.
func (m *Model) handleUserSubmit(msg userSubmitMsg) (tea.Model, tea.Cmd) {
	// Ensure any previous assistant messages are properly rendered
	m.reRenderCompletedMessages()

	// Preserve tool history from the previous turn
	m.commitToolsToLastAssistantMessage()

	// Save prompt to history and prepare for new turn
	m.addToPromptHistory(msg.content)
	m.prepareForNewTurn()

	// Add user message and placeholder assistant message
	m.messages = append(m.messages, chatMessage{
		role:    "user",
		content: msg.content,
	})
	m.messages = append(m.messages, chatMessage{
		role:        "assistant",
		content:     "",
		isStreaming: true,
	})
	m.updateViewportContent()

	// Start streaming, keep spinner ticking, and wait for events
	return m, tea.Batch(m.spinner.Tick, m.streamResponseCmd(msg.content), m.waitForEvent())
}

// commitToolsToLastAssistantMessage saves current tools to the last assistant message.
func (m *Model) commitToolsToLastAssistantMessage() {
	for i := len(m.messages) - 1; i >= 0; i-- {
		if m.messages[i].role == "assistant" {
			m.messages[i].tools = make([]*toolExecution, 0, len(m.toolOrder))
			m.messages[i].toolOrder = make([]string, len(m.toolOrder))
			copy(m.messages[i].toolOrder, m.toolOrder)
			for _, id := range m.toolOrder {
				if tool := m.tools[id]; tool != nil {
					m.messages[i].tools = append(m.messages[i].tools, tool)
				}
			}
			break
		}
	}
}

// addToPromptHistory adds a prompt to history if it's unique.
func (m *Model) addToPromptHistory(prompt string) {
	if prompt != "" && (len(m.history) == 0 || m.history[len(m.history)-1] != prompt) {
		m.history = append(m.history, prompt)
	}
	m.historyIndex = -1
	m.savedInput = ""
}

// prepareForNewTurn clears state for a new conversation turn.
func (m *Model) prepareForNewTurn() {
	m.drainEventChannel()
	m.tools = make(map[string]*toolExecution)
	m.toolOrder = make([]string, 0)
	m.sessionComplete = false
	m.pendingToolCount = 0
	m.isStreaming = true
	m.justCompleted = false
	m.userScrolled = false
	m.currentResponse.Reset()
}

// activeModalHeight returns the extra height needed for the currently active modal
// beyond the input area height (since modals replace the input area).
func (m *Model) activeModalHeight() int {
	if m.pendingPermission != nil {
		// Calculate actual permission modal height based on content
		// title(1) + blank(1) + tool(1) + blank(1) + "Allow?"(1) + blank(1) + buttons(1) = 7 base
		contentLines := 6
		if m.pendingPermission.command != "" {
			contentLines++
		}
		if m.pendingPermission.arguments != "" {
			contentLines++
		}

		// Return extra height beyond input area
		if contentLines > inputHeight {
			return contentLines - inputHeight
		}
		return 0
	}
	if m.showModelPicker || m.showSessionPicker {
		// Calculate actual picker height based on content
		var totalItems int
		if m.showSessionPicker {
			totalItems = len(m.availableSessions) + 1 // +1 for "New Chat" option
		} else {
			totalItems = len(m.availableModels) + 1 // +1 for "auto" option
		}
		visibleCount := min(totalItems, maxPickerVisible)
		isScrollable := totalItems > maxPickerVisible

		// Calculate content lines: title + visible items
		contentLines := 1 + visibleCount
		if isScrollable {
			contentLines += 2 // Add space for scroll indicators
		}
		// Apply minimum height constraint (matches calculatePickerContentLines in styles.go)
		contentLines = max(contentLines, 6)

		// Return extra height beyond input area
		if contentLines > inputHeight {
			return contentLines - inputHeight
		}
		return 0
	}
	return 0
}

// updateDimensions updates component dimensions based on terminal size.
func (m *Model) updateDimensions() {
	// Account for borders and padding
	contentWidth := m.width - 4

	// Calculate available height: total - header - input - footer - borders - modal
	// Each bordered box adds 2 lines (top + bottom border)
	modalHeight := m.activeModalHeight()
	viewportHeight := max(m.height-headerHeight-inputHeight-footerHeight-4-modalHeight, 5)

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

// getContextualHelpText returns help text based on the current active view.
func (m *Model) getContextualHelpText() string {
	// Permission prompt - highest priority
	if m.pendingPermission != nil {
		return "  y Allow • n Deny • esc Cancel"
	}

	// Model picker modal
	if m.showModelPicker {
		return "  ↑↓ Navigate • ⏎ Select • esc Cancel"
	}

	// Session picker modal
	if m.showSessionPicker {
		if m.renamingSession {
			return "  Type name • ⏎ Save • esc Cancel"
		}
		if m.confirmDeleteSession {
			return "  y Delete • n Cancel"
		}
		return "  ↑↓ Navigate • ⏎ Select • r Rename • d Delete • esc Cancel"
	}

	// Default input view - show mode and tool expand options
	modeIcon := "</>"
	if !m.agentMode {
		modeIcon = "≡"
	}
	// Check if tools are present for expand option
	hasTools := len(m.toolOrder) > 0
	if !hasTools {
		for _, msg := range m.messages {
			if msg.role == "assistant" && len(msg.tools) > 0 {
				hasTools = true
				break
			}
		}
	}
	if hasTools {
		return "  ⏎ Send • ↑↓ History • PgUp/Dn Scroll • ⇥ " + modeIcon +
			" • ^T Expand • ^H Sessions • ^O Model • ^N New • esc Quit"
	}
	return "  ⏎ Send • ↑↓ History • PgUp/Dn Scroll • ⇥ " + modeIcon +
		" • ^H Sessions • ^O Model • ^N New • esc Quit"
}

// sendMessageCmd returns a command that initiates message sending.
func (m *Model) sendMessageCmd(content string) tea.Cmd {
	return func() tea.Msg {
		return userSubmitMsg{content: content}
	}
}

// streamResponseCmd creates a command that streams the Copilot response.
// It implements the per-turn subscribe pattern: subscribe, send, events flow to channel,
// unsubscribe is stored in the model for cleanup() to call when complete.
func (m *Model) streamResponseCmd(userMessage string) tea.Cmd {
	session := m.session
	eventChan := m.eventChan
	agentMode := m.agentMode

	return func() tea.Msg {
		// Subscribe for this turn's events and store unsubscribe in the model
		unsubscribe := session.On(func(event copilot.SessionEvent) {
			switch event.Type {
			case copilot.AssistantTurnStart:
				// New turn started
				eventChan <- turnStartMsg{}
			case copilot.AssistantMessageDelta:
				// Streaming message chunk - incremental text
				if event.Data.DeltaContent != nil {
					eventChan <- streamChunkMsg{content: *event.Data.DeltaContent}
				}
			case copilot.AssistantMessage:
				// Final complete message - SDK always sends this regardless of streaming
				if event.Data.Content != nil {
					eventChan <- assistantMessageMsg{content: *event.Data.Content}
				}
			case copilot.AssistantReasoning, copilot.AssistantReasoningDelta:
				// Reasoning events - LLM is "thinking"
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
				// This is the authoritative completion signal per SDK best practices
				eventChan <- streamEndMsg{}
			case copilot.AssistantTurnEnd:
				// AssistantTurnEnd fires after each turn, including intermediate turns
				eventChan <- turnEndMsg{}
			case copilot.Abort:
				// Session aborted
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

		// Store unsubscribe in the model (thread-safe) for cleanup() to call
		m.unsubscribeMu.Lock()
		m.unsubscribe = unsubscribe
		m.unsubscribeMu.Unlock()

		// In plan mode, prefix the prompt with instructions to plan without executing
		prompt := userMessage
		if !agentMode {
			prompt = "[PLAN MODE] Research and outline steps to accomplish this task. " +
				"Do not execute tools or make changes - only describe what you would do:\n\n" +
				userMessage
		}

		// Send the message
		_, err := session.Send(copilot.MessageOptions{Prompt: prompt})
		if err != nil {
			// Clean up on error
			m.unsubscribeMu.Lock()
			if m.unsubscribe != nil {
				m.unsubscribe()
				m.unsubscribe = nil
			}
			m.unsubscribeMu.Unlock()
			return streamErrMsg{err: err}
		}

		// Event handler will send events to eventChan
		// tryFinalizeResponse() will call cleanup() when done
		return nil
	}
}

// Run starts the chat TUI and returns a permission handler for integration with the Copilot SDK.
// The returned handler can be used with SessionConfig.OnPermissionRequest to enable interactive
// permission prompting within the TUI.
func Run(
	ctx context.Context,
	session *copilot.Session,
	client *copilot.Client,
	sessionConfig *copilot.SessionConfig,
	models []copilot.ModelInfo,
	currentModel string,
	timeout time.Duration,
) error {
	model := New(session, client, sessionConfig, models, currentModel, timeout)
	model.ctx = ctx
	program := tea.NewProgram(
		model,
		tea.WithAltScreen(),
		tea.WithContext(ctx),
		tea.WithMouseCellMotion(), // Enable mouse wheel (use shift+click for text selection)
	)

	_, err := program.Run()
	return err
}

// RunWithEventChannel starts the chat TUI with a pre-created event channel.
// This allows external code (like permission handlers) to send events to the TUI.
func RunWithEventChannel(
	ctx context.Context,
	session *copilot.Session,
	client *copilot.Client,
	sessionConfig *copilot.SessionConfig,
	models []copilot.ModelInfo,
	currentModel string,
	timeout time.Duration,
	eventChan chan tea.Msg,
) error {
	model := NewWithEventChannel(
		session,
		client,
		sessionConfig,
		models,
		currentModel,
		timeout,
		eventChan,
	)
	model.ctx = ctx
	program := tea.NewProgram(
		model,
		tea.WithAltScreen(),
		tea.WithContext(ctx),
		tea.WithMouseCellMotion(), // Enable mouse wheel (use shift+click for text selection)
	)

	_, err := program.Run()
	return err
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
