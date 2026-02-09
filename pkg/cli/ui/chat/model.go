package chat

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	copilot "github.com/github/copilot-sdk/go"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

const (
	defaultWidth  = 100
	defaultHeight = 30
	inputHeight   = 3
	headerHeight  = logoHeight + 3 // logo + tagline + border
	footerHeight  = 1              // single line help text

	// Shared picker/output constants.
	maxPickerItems    = 10 // absolute maximum items in picker modals
	minPickerItems    = 3  // minimum items to show in picker modals
	minViewportHeight = 10 // minimum height to preserve for main viewport
	minWrapWidth      = 20 // minimum width for text wrapping
	viewSectionCount  = 4  // number of sections in View: header, viewport, input, footer

	// Tool and error message constants.
	unknownToolName  = "unknown"           // fallback when tool name is nil
	unknownErrorMsg  = "unknown error"     // fallback when error message is nil
	toolIDFormat     = "tool-%d"           // format string for generating unique tool IDs (timestamp-based)
	unknownOperation = "Unknown Operation" // fallback when operation type is unrecognized
	planModePrefix   = "[PLAN MODE] Research and outline steps to accomplish this task. " +
		"Do not execute tools or make changes - only describe what you would do:\n\n"
)

// AgentModeRef is a thread-safe reference to the agent mode state.
// It allows tool handlers to check the current mode at execution time.
// The enabled field is unexported to ensure all access goes through mutex-protected methods.
type AgentModeRef struct {
	mu      sync.RWMutex
	enabled bool
}

// NewAgentModeRef creates a new AgentModeRef with the given initial state.
func NewAgentModeRef(initial bool) *AgentModeRef {
	return &AgentModeRef{
		enabled: initial,
	}
}

// IsEnabled returns true if agent mode is enabled (tools can execute).
func (r *AgentModeRef) IsEnabled() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.enabled
}

// SetEnabled updates the agent mode state.
func (r *AgentModeRef) SetEnabled(enabled bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.enabled = enabled
}

// chatMessage represents a single message in the chat history.
type chatMessage struct {
	role        string // "user", "assistant", or "tool"
	content     string
	rendered    string // markdown-rendered content for assistant messages
	isStreaming bool
	tools       []*toolExecution // tools executed during this assistant message
	toolOrder   []string         // ordered tool IDs for this message
	agentMode   bool             // true = agent mode, false = plan mode (for user messages)
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
	messages         []chatMessage
	currentResponse  strings.Builder
	isStreaming      bool
	justCompleted    bool // true when a response just finished, shows "Ready" indicator
	showCopyFeedback bool // true when copy feedback should be shown briefly
	userScrolled     bool // true when user has scrolled away from bottom (pause auto-scroll)
	err              error
	quitting         bool
	ready            bool

	// Prompt history
	history      []string // previously submitted prompts
	historyIndex int      // -1 means not browsing history, 0+ is position in history
	savedInput   string   // saves current input when browsing history

	// Tool execution tracking
	tools            map[string]*toolExecution // keyed by tool ID
	toolOrder        []string                  // ordered list of tool IDs for rendering
	pendingToolCount int                       // number of tools awaiting completion

	// Session completion tracking
	sessionComplete bool       // true when SessionIdle has been received
	unsubscribe     func()     // function to unsubscribe from session events
	unsubscribeMu   sync.Mutex // protects unsubscribe access

	// Dimensions
	width  int
	height int

	// Help system
	help            help.Model // bubbles/help model for rendering help
	keys            KeyMap     // structured keybindings
	showHelpOverlay bool       // true when help overlay is visible

	// Copilot session and model switching
	session       *copilot.Session
	client        *copilot.Client
	sessionConfig *copilot.SessionConfig
	timeout       time.Duration
	ctx           context.Context

	// Model selection
	currentModel      string              // currently selected model ID
	availableModels   []copilot.ModelInfo // models the user has access to
	filteredModels    []copilot.ModelInfo // models matching current filter
	showModelPicker   bool                // true when model picker overlay is visible
	modelPickerIndex  int                 // currently highlighted model in picker
	modelFilterActive bool                // true when filter input is focused
	modelFilterText   string              // current filter text

	// Permission request handling
	pendingPermission *permissionRequestMsg // current permission request awaiting user response
	permissionHistory []permissionResponse  // history of permission decisions

	// Session management
	currentSessionID     string            // ID of the current session (empty if new)
	availableSessions    []SessionMetadata // cached list of available sessions
	filteredSessions     []SessionMetadata // sessions matching current filter
	showSessionPicker    bool              // true when session picker overlay is visible
	sessionPickerIndex   int               // currently highlighted session in picker
	confirmDeleteSession bool              // true when confirming session deletion
	renamingSession      bool              // true when renaming a session
	sessionRenameInput   string            // current rename input text
	sessionFilterActive  bool              // true when filter input is focused
	sessionFilterText    string            // current filter text

	// Markdown renderer (cached to avoid terminal queries)
	renderer *glamour.TermRenderer

	// Mode selection (agent executes tools, plan describes only)
	agentMode    bool          // true = agent (execute), false = plan (describe only)
	agentModeRef *AgentModeRef // shared reference for tool handlers to check current mode

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
	return NewWithEventChannel(
		session,
		client,
		sessionConfig,
		models,
		currentModel,
		timeout,
		nil,
		nil,
	)
}

// NewWithEventChannel creates a new chat TUI model with an optional pre-existing event channel.
// If eventChan is nil, a new channel is created. This allows external code to send events
// to the TUI (e.g., permission requests).
// If agentModeRef is provided, it will be used to synchronize agent mode state with tool handlers.
func NewWithEventChannel(
	session *copilot.Session,
	client *copilot.Client,
	sessionConfig *copilot.SessionConfig,
	models []copilot.ModelInfo,
	currentModel string,
	timeout time.Duration,
	eventChan chan tea.Msg,
	agentModeRef *AgentModeRef,
) *Model {
	textArea := createTextArea()
	viewPort := createViewport()

	// Initialize spinner
	spin := spinner.New()
	spin.Spinner = spinner.MiniDot
	spin.Style = spinnerStyle

	// Initialize markdown renderer before Bubbletea takes over terminal
	// This avoids terminal queries that could interfere with input
	mdRenderer := createRenderer(defaultWidth - rendererPadding)

	// Use provided event channel or create new one
	if eventChan == nil {
		eventChan = make(chan tea.Msg, eventChanBuf)
	}

	return &Model{
		viewport:         viewPort,
		textarea:         textArea,
		spinner:          spin,
		renderer:         mdRenderer,
		help:             createHelpModel(),
		keys:             DefaultKeyMap(),
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
		agentMode:        true,         // Default to agent mode
		agentModeRef:     agentModeRef, // Store reference for tool handlers
	}
}

// createTextArea initializes the textarea component for user input.
func createTextArea() textarea.Model {
	textArea := textarea.New()
	textArea.Placeholder = "Ask me anything about Kubernetes, KSail, or cluster management..."
	textArea.Focus()
	textArea.CharLimit = charLimit
	textArea.SetWidth(defaultWidth - textAreaPadding)
	textArea.SetHeight(inputHeight)
	textArea.ShowLineNumbers = false

	// Show ">" only on first line, nothing on continuation lines
	textArea.SetPromptFunc(modalPadding, func(lineIdx int) string {
		if lineIdx == 0 {
			return "> "
		}

		return "  "
	})
	textArea.FocusedStyle.CursorLine = lipgloss.NewStyle()
	textArea.FocusedStyle.Placeholder = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))

	return textArea
}

// createViewport initializes the viewport component for chat history.
func createViewport() viewport.Model {
	viewportWidth := defaultWidth - viewportPadding
	viewportHeight := defaultHeight - inputHeight - headerHeight - footerHeight - viewportPadding
	viewPort := viewport.New(viewportWidth, viewportHeight)
	initialMsg := "  Type a message below to start chatting with KSail AI.\n"
	viewPort.SetContent(statusStyle.Render(initialMsg))

	return viewPort
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
//
//nolint:cyclop // type-switch dispatcher for tea.Msg
func (m *Model) Update(
	msg tea.Msg,
) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m.handleKeyMsg(msg)

	case tea.MouseMsg:
		return m.handleMouseMsg(msg)

	case tea.WindowSizeMsg:
		m.handleWindowSize(msg)

	case userSubmitMsg:
		return m.handleUserSubmit(msg)

	case streamChunkMsg, assistantMessageMsg, toolStartMsg, toolEndMsg,
		toolOutputChunkMsg, ToolOutputChunkMsg, permissionRequestMsg,
		PermissionRequestMsg, streamEndMsg, turnStartMsg, turnEndMsg,
		reasoningMsg, abortMsg, snapshotRewindMsg, streamErrMsg:
		return m.handleStreamEvent(msg)

	case copyFeedbackClearMsg:
		m.showCopyFeedback = false

		return m, nil

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

	sections := make([]string, 0, viewSectionCount)

	// Header, chat viewport, input/modal, and footer
	sections = append(sections, m.renderHeader())
	sections = append(sections, viewportStyle.Width(m.width-modalPadding).Render(m.viewport.View()))
	sections = append(sections, m.renderInputOrModal())
	sections = append(sections, m.renderFooter())

	// Join sections and clip final output to terminal width to prevent any wrapping
	output := lipgloss.JoinVertical(lipgloss.Left, sections...)

	return lipgloss.NewStyle().MaxWidth(m.width).Render(output)
}

// handleStreamEvent dispatches streaming-related events to their specific handlers.
//
//nolint:cyclop // type-switch dispatcher for stream messages
func (m *Model) handleStreamEvent(
	msg tea.Msg,
) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
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

	case snapshotRewindMsg:
		return m.handleSnapshotRewind()

	case streamErrMsg:
		return m.handleStreamErr(msg)

	default:
		return m, nil
	}
}

// handleMouseMsg handles mouse input events.
func (m *Model) handleMouseMsg(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	//nolint:exhaustive // Only wheel events are relevant for viewport scrolling.
	switch msg.Button {
	case tea.MouseButtonWheelUp:
		m.viewport.ScrollUp(scrollLines)
		m.userScrolled = !m.viewport.AtBottom()
	case tea.MouseButtonWheelDown:
		m.viewport.ScrollDown(scrollLines)

		if m.viewport.AtBottom() {
			m.userScrolled = false
		}
	default:
		// Ignore other mouse events (clicks, drags) - terminal handles text selection
	}

	return m, nil
}

// handleWindowSize processes terminal resize events.
func (m *Model) handleWindowSize(msg tea.WindowSizeMsg) {
	m.width = msg.Width
	m.height = msg.Height
	m.updateDimensions()

	if !m.ready {
		m.ready = true
	}
}

// calculateMaxPickerVisible returns the maximum number of items that can be shown
// in a picker modal without pushing the viewport out of view.
func (m *Model) calculateMaxPickerVisible() int {
	// Calculate available height: total - header - input - footer - borders
	// Reserve space for: title (1) + scroll indicators (2) + borders (2) + minimum viewport
	availableHeight := m.height - headerHeight - inputHeight - footerHeight - viewportPadding - minViewportHeight

	// Subtract space for picker overhead (title + top/bottom padding)
	availableForItems := availableHeight - pickerOverhead

	// Calculate max items: cap between min and max
	maxItems := max(minPickerItems, min(availableForItems, maxPickerItems))

	return maxItems
}

// renderHeader renders the header section with logo and status.
func (m *Model) renderHeader() string {
	headerContentWidth := max(m.width-headerPadding, 1)

	// Truncate each logo line by display width (handles Unicode properly)
	logoLines := strings.Split(logo(), "\n")
	truncateStyle := lipgloss.NewStyle().MaxWidth(headerContentWidth).Inline(true)

	var clippedLogo strings.Builder

	for idx, line := range logoLines {
		clippedLine := truncateStyle.Render(line)
		clippedLogo.WriteString(clippedLine)

		if idx < len(logoLines)-1 {
			clippedLogo.WriteString("\n")
		}
	}

	logoRendered := logoStyle.Render(clippedLogo.String())

	// Build tagline with right-aligned status
	taglineRow := m.buildTaglineRow(headerContentWidth)
	taglineRow = lipgloss.NewStyle().MaxWidth(headerContentWidth).Inline(true).Render(taglineRow)

	headerContent := logoRendered + "\n" + taglineRow

	return headerBoxStyle.Width(m.width - modalPadding).Render(headerContent)
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
	spacing := max(contentWidth-taglineLen-statusLen, minSpacing)

	return taglineText + strings.Repeat(" ", spacing) + statusText
}

// buildStatusText builds the status indicator text (mode, model, streaming state).
func (m *Model) buildStatusText() string {
	var statusParts []string

	// Mode icon: </> for Agent, ≡ for Plan
	modeStyle := lipgloss.NewStyle().Foreground(lipgloss.ANSIColor(ansiCyan))
	if m.agentMode {
		statusParts = append(statusParts, modeStyle.Render("</>"))
	} else {
		statusParts = append(statusParts, modeStyle.Render("≡"))
	}

	// Model name
	modelStyle := lipgloss.NewStyle().Foreground(dimColor)

	switch {
	case m.currentModel != "":
		statusParts = append(statusParts, modelStyle.Render(m.currentModel))
	default:
		statusParts = append(statusParts, modelStyle.Render(modelAuto))
	}

	// Streaming state and feedback
	switch {
	case m.isStreaming:
		statusParts = append(statusParts, m.spinner.View()+" "+statusStyle.Render("Thinking..."))
	case m.showCopyFeedback:
		statusParts = append(statusParts, statusStyle.Render("Copied ✓"))
	case m.justCompleted:
		statusParts = append(statusParts, statusStyle.Render("Ready ✓"))
	}

	return strings.Join(statusParts, " • ")
}

// renderInputOrModal renders either the input area or active modal.
func (m *Model) renderInputOrModal() string {
	if m.showHelpOverlay {
		return m.renderHelpOverlay()
	}

	if m.pendingPermission != nil {
		return m.renderPermissionModal()
	}

	if m.showModelPicker {
		return m.renderModelPickerModal()
	}

	if m.showSessionPicker {
		return m.renderSessionPickerModal()
	}

	return inputStyle.Width(m.width - modalPadding).Render(m.textarea.View())
}

// renderFooter renders the context-aware help text footer using bubbles/help.
func (m *Model) renderFooter() string {
	return lipgloss.NewStyle().MaxWidth(m.width).Inline(true).Render(m.renderShortHelp())
}

// handleUserSubmit handles user message submission.
func (m *Model) handleUserSubmit(msg userSubmitMsg) (tea.Model, tea.Cmd) {
	// Ensure any previous assistant messages are properly rendered
	m.reRenderCompletedMessages()

	// Preserve tool history from the previous turn
	m.commitToolsToLastAssistantMessage()

	// Save original prompt to history and prepare for new turn
	m.addToPromptHistory(msg.content)
	m.prepareForNewTurn()

	// Add user message and placeholder assistant message
	// Store the current mode with the message so it can be displayed in the indicator
	m.messages = append(m.messages, chatMessage{
		role:      roleUser,
		content:   msg.content,
		agentMode: m.agentMode,
	})
	m.messages = append(m.messages, chatMessage{
		role:        roleAssistant,
		content:     "",
		isStreaming: true,
	})
	m.updateViewportContent()

	// Start streaming, keep spinner ticking, and wait for events
	// streamResponseCmd will send the full plan mode instruction to the model
	return m, tea.Batch(m.spinner.Tick, m.streamResponseCmd(msg.content), m.waitForEvent())
}

// commitToolsToLastAssistantMessage saves current tools to the last assistant message.
func (m *Model) commitToolsToLastAssistantMessage() {
	for idx := len(m.messages) - 1; idx >= 0; idx-- {
		if m.messages[idx].role == roleAssistant {
			m.messages[idx].tools = make([]*toolExecution, 0, len(m.toolOrder))
			m.messages[idx].toolOrder = make([]string, len(m.toolOrder))
			copy(m.messages[idx].toolOrder, m.toolOrder)

			for _, id := range m.toolOrder {
				if tool := m.tools[id]; tool != nil {
					m.messages[idx].tools = append(m.messages[idx].tools, tool)
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
	switch {
	case m.showHelpOverlay:
		return 0
	case m.pendingPermission != nil:
		return m.permissionModalExtraHeight()
	case m.showModelPicker || m.showSessionPicker:
		return m.pickerModalExtraHeight()
	default:
		return 0
	}
}

// permissionModalExtraHeight calculates the extra height for the permission modal.
func (m *Model) permissionModalExtraHeight() int {
	contentLines := permissionBaseLines
	if m.pendingPermission.command != "" {
		contentLines++
	}

	if m.pendingPermission.arguments != "" {
		contentLines++
	}

	if contentLines > inputHeight {
		return contentLines - inputHeight
	}

	return 0
}

// pickerModalExtraHeight calculates the extra height for picker modals.
func (m *Model) pickerModalExtraHeight() int {
	var totalItems int
	if m.showSessionPicker {
		totalItems = len(m.filteredSessions) + 1 // +1 for "New Chat" option
	} else {
		totalItems = len(m.filteredModels) + 1 // +1 for "auto" option
	}

	maxVisible := m.calculateMaxPickerVisible()
	visibleCount := min(totalItems, maxVisible)
	isScrollable := totalItems > maxVisible

	// Calculate content lines: title + visible items
	contentLines := 1 + visibleCount
	if isScrollable {
		contentLines += modalPadding // Add space for scroll indicators
	}

	// Apply minimum height constraint
	contentLines = max(contentLines, minPickerHeight)

	if contentLines > inputHeight {
		return contentLines - inputHeight
	}

	return 0
}

// updateDimensions updates component dimensions based on terminal size.
func (m *Model) updateDimensions() {
	// Account for borders and padding
	contentWidth := m.width - viewportPadding

	// Calculate available height: total - header - input - footer - borders - modal
	// Each bordered box adds 2 lines (top + bottom border)
	modalHeight := m.activeModalHeight()
	viewportHeight := max(
		m.height-headerHeight-inputHeight-footerHeight-viewportPadding-modalHeight,
		minHeight,
	)

	oldWidth := m.viewport.Width
	m.viewport.Width = contentWidth - viewportInner
	m.viewport.Height = viewportHeight
	m.textarea.SetWidth(contentWidth - viewportInner)

	// If viewport width changed, recreate the renderer and re-render completed messages
	if oldWidth != m.viewport.Width {
		m.renderer = createRenderer(m.viewport.Width - rendererMinWidth)
		m.reRenderCompletedMessages()
		m.updateViewportContent()
	}
}

// reRenderCompletedMessages re-renders all completed assistant messages with the current renderer.
func (m *Model) reRenderCompletedMessages() {
	for idx := range m.messages {
		msg := &m.messages[idx]
		if msg.role == roleAssistant && msg.content != "" {
			msg.rendered = renderMarkdownWithRenderer(m.renderer, msg.content)
		}
	}
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
		// Create event dispatcher to route SDK events to tea messages
		dispatcher := newSessionEventDispatcher(eventChan)

		// Subscribe for this turn's events and store unsubscribe in the model
		unsubscribe := session.On(dispatcher.dispatch)

		// Store unsubscribe in the model (thread-safe) for cleanup() to call
		m.unsubscribeMu.Lock()
		m.unsubscribe = unsubscribe
		m.unsubscribeMu.Unlock()

		// In plan mode, prefix the prompt with instructions to plan without executing
		prompt := userMessage
		if !agentMode {
			prompt = planModePrefix + userMessage
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
	return RunWithEventChannelAndModeRef(
		ctx,
		session,
		client,
		sessionConfig,
		models,
		currentModel,
		timeout,
		nil,
		nil,
	)
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
	return RunWithEventChannelAndModeRef(
		ctx,
		session,
		client,
		sessionConfig,
		models,
		currentModel,
		timeout,
		eventChan,
		nil,
	)
}

// RunWithEventChannelAndModeRef starts the chat TUI with a pre-created event channel and agent mode reference.
// This allows external code (like permission handlers) to send events to the TUI and synchronize agent mode state.
func RunWithEventChannelAndModeRef(
	ctx context.Context,
	session *copilot.Session,
	client *copilot.Client,
	sessionConfig *copilot.SessionConfig,
	models []copilot.ModelInfo,
	currentModel string,
	timeout time.Duration,
	eventChan chan tea.Msg,
	agentModeRef *AgentModeRef,
) error {
	model := NewWithEventChannel(
		session,
		client,
		sessionConfig,
		models,
		currentModel,
		timeout,
		eventChan,
		agentModeRef,
	)
	model.ctx = ctx

	// Ensure agentModeRef is initialized with the model's initial state
	if agentModeRef != nil {
		agentModeRef.SetEnabled(model.agentMode)
	}

	program := tea.NewProgram(
		model,
		tea.WithAltScreen(),
		tea.WithContext(ctx),
		tea.WithMouseCellMotion(), // Enable mouse wheel (use shift+click for text selection)
	)

	_, err := program.Run()
	if err != nil {
		return fmt.Errorf("running chat program with event channel: %w", err)
	}

	return nil
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
		// Extract tool name and command from the permission request.
		// The Extra map contains the raw permission request data from the SDK.
		// Common keys vary by permission kind (shell, file_edit, etc.)
		toolName, command := extractPermissionDetails(request)

		// Create response channel
		responseChan := make(chan bool, 1)

		// Send permission request to TUI
		eventChan <- permissionRequestMsg{
			toolCallID: request.ToolCallID,
			toolName:   toolName,
			command:    command,
			arguments:  "", // Arguments are included in the command for SDK permissions
			response:   responseChan,
		}

		// Wait for response from TUI
		approved := <-responseChan

		if approved {
			return copilot.PermissionRequestResult{Kind: "approved"}, nil
		}

		return copilot.PermissionRequestResult{Kind: "denied-interactively-by-user"}, nil
	}
}

// extractPermissionDetails extracts human-readable tool name and command from an SDK permission request.
// The Extra map contains different fields depending on the permission kind.
// Uses priority-based field checking with early returns to avoid deep nesting.
func extractPermissionDetails(request copilot.PermissionRequest) (string, string) {
	// Default tool name based on permission kind
	toolName := formatPermissionKind(request.Kind)

	// Try command fields first, then nested execution, then path fields, then fallback
	if cmd := findCommandInMap(request.Extra); cmd != "" {
		return toolName, cmd
	}

	if cmd := findCommandInExecution(request.Extra); cmd != "" {
		return toolName, cmd
	}

	if path := findPathInMap(request.Extra); path != "" {
		return toolName, path
	}

	if cmd := findFallbackValue(request.Extra); cmd != "" {
		return toolName, cmd
	}

	// Last resort: just show the permission kind
	return toolName, request.Kind
}

// commandFields contains field names that typically hold the command to execute.
var commandFields = []string{
	"command", "cmd", "shell", "runInTerminal", "execute", "run", "script", "input",
}

// pathFields contains field names for file path operations.
var pathFields = []string{"path", "filePath", "file", "target"}

// metadataFields contains field names to skip during fallback search.
var metadataFields = map[string]bool{
	"kind": true, "toolCallId": true, "possiblePaths": true,
	"possibleUrls": true, "sessionId": true, "requestId": true,
	"timestamp": true, "type": true, "id": true,
}

// findCommandInMap searches for a command value in the given map.
func findCommandInMap(extra map[string]any) string {
	for _, field := range commandFields {
		if val, ok := extra[field]; ok {
			if cmd := extractStringValue(val); cmd != "" {
				return cmd
			}
		}
	}

	return ""
}

// findCommandInExecution checks for a nested execution object with command fields.
func findCommandInExecution(extra map[string]any) string {
	exec, ok := extra["execution"].(map[string]any)
	if !ok {
		return ""
	}

	return findCommandInMap(exec)
}

// findPathInMap searches for a path value in the given map.
func findPathInMap(extra map[string]any) string {
	for _, field := range pathFields {
		if val, ok := extra[field]; ok {
			if path := extractStringValue(val); path != "" {
				return path
			}
		}
	}

	return ""
}

// findFallbackValue searches for any non-metadata string value in the given map.
func findFallbackValue(extra map[string]any) string {
	for key, val := range extra {
		if metadataFields[key] {
			continue
		}

		if cmd := extractStringValue(val); cmd != "" {
			return cmd
		}
	}

	return ""
}

// extractStringValue extracts a string from various value types.
func extractStringValue(val any) string {
	switch typedVal := val.(type) {
	case string:
		return typedVal
	case []any:
		// Join array elements
		parts := make([]string, 0, len(typedVal))
		for _, item := range typedVal {
			if s, ok := item.(string); ok && s != "" {
				parts = append(parts, s)
			}
		}

		if len(parts) > 0 {
			return strings.Join(parts, " ")
		}
	case map[string]any:
		// Try to extract command from nested object
		if cmd, ok := typedVal["command"].(string); ok {
			return cmd
		}

		if cmd, ok := typedVal["cmd"].(string); ok {
			return cmd
		}
	}

	return ""
}

// formatPermissionKind converts a permission kind to a human-readable tool name.
func formatPermissionKind(kind string) string {
	switch kind {
	case "shell":
		return "Shell Command"
	case "file_edit", "fileEdit":
		return "File Edit"
	case "file_read", "fileRead":
		return "File Read"
	case "file_write", "fileWrite":
		return "File Write"
	case "terminal":
		return "Terminal"
	case "browser":
		return "Browser"
	case "network":
		return "Network Request"
	default:
		// Capitalize and format the kind
		if kind == "" {
			return unknownOperation
		}
		// Replace underscores with spaces and title case.
		// English titlecase is appropriate for all SDK permission kinds (shell, file_edit, etc.)
		formatted := strings.ReplaceAll(kind, "_", " ")
		caser := cases.Title(language.English)

		return caser.String(formatted)
	}
}
