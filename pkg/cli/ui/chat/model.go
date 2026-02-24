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
)

const (
	defaultWidth  = 100
	defaultHeight = 30
	inputHeight   = 3
	footerHeight  = 1 // single line help text

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
	planModePrefix   = `[PLAN MODE] Research and outline steps to accomplish this task. ` +
		`Do not execute tools or make changes - only describe what you would do:


`
	askModePrefix = `[ASK MODE] You are in read-only investigation mode. ` +
		`Use available read-only tools to research and gather information, then provide a thorough answer. ` +
		`Do NOT execute any tools that create, modify, or delete resources:


`
)

// ChatMode represents the chat interaction mode.
type ChatMode int //nolint:revive // ChatMode is clearer than Mode given existing ModeRef type in this package.

const (
	// AgentMode allows full tool execution with permission prompts for write operations.
	AgentMode ChatMode = iota
	// PlanMode blocks all tool execution; the model describes what it would do.
	PlanMode
	// AskMode allows read-only tool execution; write tools are blocked.
	AskMode
)

// String returns a human-readable label for the chat mode.
func (m ChatMode) String() string {
	switch m {
	case AgentMode:
		return "agent"
	case PlanMode:
		return "plan"
	case AskMode:
		return "ask"
	default:
		return "agent"
	}
}

// Icon returns the TUI icon for the chat mode.
func (m ChatMode) Icon() string {
	switch m {
	case AgentMode:
		return "</>"
	case PlanMode:
		return "\u2261" // ≡
	case AskMode:
		return "?"
	default:
		return "</>"
	}
}

// Label returns the icon and text label for the chat mode (e.g. "</> agent").
func (m ChatMode) Label() string {
	return m.Icon() + " " + m.String()
}

// Next cycles to the next chat mode: Agent -> Plan -> Ask -> Agent.
func (m ChatMode) Next() ChatMode {
	switch m {
	case AgentMode:
		return PlanMode
	case PlanMode:
		return AskMode
	case AskMode:
		return AgentMode
	default:
		return AgentMode
	}
}

// ModeRef is a thread-safe reference to a boolean mode state.
// It allows tool handlers to check and update mode state at execution time.
// The enabled field is unexported to ensure all access goes through mutex-protected methods.
type ModeRef struct {
	mu      sync.RWMutex
	enabled bool
}

// NewModeRef creates a new ModeRef with the given initial state.
func NewModeRef(initial bool) *ModeRef {
	return &ModeRef{
		enabled: initial,
	}
}

// IsEnabled returns true if the mode is enabled.
func (r *ModeRef) IsEnabled() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.enabled
}

// SetEnabled updates the mode state.
func (r *ModeRef) SetEnabled(enabled bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.enabled = enabled
}

// ChatModeRef is a thread-safe reference to the current chat mode.
// It allows tool handlers to check the mode at execution time.
//
//nolint:revive // ChatModeRef avoids ambiguity with existing ModeRef type.
type ChatModeRef struct {
	mu   sync.RWMutex
	mode ChatMode
}

// NewChatModeRef creates a new ChatModeRef with the given initial mode.
func NewChatModeRef(initial ChatMode) *ChatModeRef {
	return &ChatModeRef{
		mode: initial,
	}
}

// Mode returns the current chat mode.
func (r *ChatModeRef) Mode() ChatMode {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.mode
}

// SetMode updates the chat mode.
func (r *ChatModeRef) SetMode(mode ChatMode) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.mode = mode
}

// YoloModeRef is a thread-safe reference to the YOLO mode state.
// When enabled, write operations are auto-approved without prompting the user.
type YoloModeRef = ModeRef

// NewYoloModeRef creates a new YoloModeRef with the given initial state.
func NewYoloModeRef(initial bool) *YoloModeRef {
	return NewModeRef(initial)
}

// message represents a single message in the chat history.
type message struct {
	role        string // "user", "assistant", or "tool"
	content     string
	rendered    string // markdown-rendered content for assistant messages
	isStreaming bool
	tools       []*toolExecution // tools executed during this assistant message
	toolOrder   []string         // ordered tool IDs for this message
	chatMode    ChatMode         // the chat mode when this message was sent (for user messages)
}

// permissionResponse records a user's response to a permission request.
type permissionResponse struct {
	toolName string
	command  string
	allowed  bool
}

// pendingPrompt represents a prompt that has been queued but not yet sent.
// It captures the full state at the time of queuing so that mode changes
// after queuing don't affect the prompt's execution.
type pendingPrompt struct {
	content         string   // the prompt text
	chatMode        ChatMode // agent/plan/ask mode when queued
	model           string   // model ID when queued
	reasoningEffort string   // reasoning effort when queued (if applicable)
}

// Model is the Bubbletea model for the chat TUI.
type Model struct {
	// Components
	viewport viewport.Model
	textarea textarea.Model
	spinner  spinner.Model

	// State
	messages                     []message
	currentResponse              strings.Builder
	isStreaming                  bool
	justCompleted                bool   // true when a response just finished, shows "Ready" indicator
	showCopyFeedback             bool   // true when copy feedback should be shown briefly
	showModelUnavailableFeedback bool   // true when model-unavailable feedback should be shown
	modelUnavailableReason       string // reason why models are unavailable (for status bar)
	userScrolled                 bool   // true when user has scrolled away from bottom (pause auto-scroll)
	err                          error
	quitting                     bool
	ready                        bool

	// Configuration
	theme        ThemeConfig
	toolDisplay  ToolDisplayConfig
	styles       uiStyles
	headerHeight int

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

	// Token usage tracking (updated via AssistantUsage events)
	lastUsageModel   string  // model used in the last usage event
	lastInputTokens  float64 // input tokens from the last usage event
	lastOutputTokens float64 // output tokens from the last usage event
	lastCost         float64 // cost from the last usage event

	// Quota tracking (updated via QuotaSnapshots in AssistantUsage events)
	lastQuotaSnapshots map[string]quotaSnapshot // keyed by quota category (e.g., "premium")

	// Compaction state (updated via SessionCompaction events)
	isCompacting bool // true while context compaction is in progress

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

	// Reasoning effort selection
	showReasoningPicker  bool // true when reasoning effort picker overlay is visible
	reasoningPickerIndex int  // currently highlighted effort level in picker

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

	// Mode selection (agent executes tools, plan describes only, ask is read-only)
	chatMode    ChatMode     // current chat mode
	chatModeRef *ChatModeRef // shared reference for tool handlers to check current mode

	// YOLO mode (auto-approve write operations without prompting)
	yoloMode    bool         // true = auto-approve, false = prompt for confirmation
	yoloModeRef *YoloModeRef // shared reference for tool handlers to check YOLO state

	// Prompt queuing and steering
	queuedPrompts   []pendingPrompt // FIFO queue for prompts to process after current turn
	steeringPrompts []pendingPrompt // steering prompts to inject when session becomes idle

	// Channel for async streaming events from Copilot
	eventChan chan tea.Msg
}

// NewModel creates a new chat TUI model from the given parameters.
// If params.EventChan is nil, a new channel is created.
// If params.Theme or params.ToolDisplay are zero-valued, defaults are applied.
func NewModel(params Params) *Model {
	const headerPadding = 3

	theme := params.Theme
	if theme.Logo == nil {
		theme = DefaultThemeConfig()
	}

	toolDisplay := params.ToolDisplay
	if toolDisplay.NameMappings == nil {
		toolDisplay = DefaultToolDisplayConfig()
	}

	styles := newUIStyles(theme)
	headerH := theme.LogoHeight + headerPadding
	textArea := createTextArea(theme.Placeholder)
	viewPort := createViewport(theme.WelcomeMessage, styles.status, headerH)

	// Initialize spinner
	spin := spinner.New()
	spin.Spinner = spinner.MiniDot
	spin.Style = styles.spinner

	// Initialize markdown renderer before Bubbletea takes over terminal
	// This avoids terminal queries that could interfere with input
	mdRenderer := createRenderer(defaultWidth - rendererPadding)

	// Use provided event channel or create new one
	eventChan := params.EventChan
	if eventChan == nil {
		eventChan = make(chan tea.Msg, eventChanBuf)
	}

	return &Model{
		theme:            theme,
		toolDisplay:      toolDisplay,
		styles:           styles,
		headerHeight:     headerH,
		viewport:         viewPort,
		textarea:         textArea,
		spinner:          spin,
		renderer:         mdRenderer,
		help:             createHelpModel(styles),
		keys:             DefaultKeyMap(),
		messages:         make([]message, 0),
		session:          params.Session,
		client:           params.Client,
		sessionConfig:    params.SessionConfig,
		currentSessionID: params.Session.SessionID, // Track the SDK's session ID
		timeout:          params.Timeout,
		ctx:              context.Background(),
		eventChan:        eventChan,
		width:            defaultWidth,
		height:           defaultHeight,
		tools:            make(map[string]*toolExecution),
		toolOrder:        make([]string, 0),
		history:          make([]string, 0),
		historyIndex:     -1,
		availableModels:  params.Models,
		currentModel:     params.CurrentModel,
		chatMode:         AgentMode,          // Default to agent mode
		chatModeRef:      params.ChatModeRef, // Store reference for tool handlers
		yoloModeRef:      params.YoloModeRef, // Store reference for YOLO mode
		queuedPrompts:    make([]pendingPrompt, 0),
		steeringPrompts:  make([]pendingPrompt, 0),
	}
}

// createTextArea initializes the textarea component for user input.
func createTextArea(placeholder string) textarea.Model {
	textArea := textarea.New()
	textArea.Placeholder = placeholder
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
func createViewport(welcomeMessage string, statusSty lipgloss.Style, headerH int) viewport.Model {
	viewportWidth := defaultWidth - viewportWidthPadding
	viewportHeight := defaultHeight - inputHeight - headerH - footerHeight - viewportHeightPadding
	viewPort := viewport.New(viewportWidth, viewportHeight)
	initialMsg := "  " + welcomeMessage + "\n"
	viewPort.SetContent(statusSty.Render(initialMsg))

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
		reasoningMsg, abortMsg, snapshotRewindMsg, streamErrMsg,
		usageMsg, compactionStartMsg, compactionCompleteMsg,
		intentMsg, modelChangeMsg, shutdownMsg:
		return m.handleStreamEvent(msg)

	case copyFeedbackClearMsg:
		m.showCopyFeedback = false

		return m, nil

	case modelUnavailableClearMsg:
		m.showModelUnavailableFeedback = false
		m.modelUnavailableReason = ""

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
		goodbye := m.styles.status.Render("  " + m.theme.GoodbyeMessage + "\n")

		return m.styles.logo.Render(m.theme.Logo()) + "\n\n" + goodbye
	}

	sections := make([]string, 0, viewSectionCount)

	// Header, chat viewport, input/modal, and footer
	sections = append(sections, m.renderHeader())
	sections = append(sections,
		m.styles.viewport.Width(max(m.width-modalPadding, 1)).Render(m.viewport.View()),
	)
	sections = append(sections, m.renderInputOrModal())
	sections = append(sections, m.renderFooter())

	// Join sections and clip final output to terminal width to prevent any wrapping
	output := lipgloss.JoinVertical(lipgloss.Left, sections...)

	return lipgloss.NewStyle().MaxWidth(m.width).Render(output)
}

// handleStreamEvent dispatches streaming-related events to their specific handlers.
//
//nolint:cyclop,funlen // type-switch dispatcher for stream messages
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

	case usageMsg:
		return m.handleUsage(msg)

	case compactionStartMsg:
		return m.handleCompactionStart()

	case compactionCompleteMsg:
		return m.handleCompactionComplete(msg)

	case intentMsg:
		return m.handleIntent(msg)

	case modelChangeMsg:
		return m.handleModelChange(msg)

	case shutdownMsg:
		return m.handleShutdown(msg)

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
	m.messages = append(m.messages, message{
		role:     roleUser,
		content:  msg.content,
		chatMode: m.chatMode,
	})
	m.messages = append(m.messages, message{
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
	chatMode := m.chatMode
	commandBuilders := m.toolDisplay.CommandBuilders

	return func() tea.Msg {
		// Create event dispatcher to route SDK events to tea messages
		dispatcher := newSessionEventDispatcher(eventChan, commandBuilders)

		// Subscribe for this turn's events and store unsubscribe in the model
		unsubscribe := session.On(dispatcher.dispatch)

		// Store unsubscribe in the model (thread-safe) for cleanup() to call
		m.unsubscribeMu.Lock()
		m.unsubscribe = unsubscribe
		m.unsubscribeMu.Unlock()

		// In plan or ask mode, prefix the prompt with mode-specific instructions
		prompt := userMessage

		switch chatMode {
		case PlanMode:
			prompt = planModePrefix + userMessage
		case AskMode:
			prompt = askModePrefix + userMessage
		case AgentMode:
			// No prefix needed
		}

		// Send the message
		_, err := session.Send(m.ctx, copilot.MessageOptions{Prompt: prompt})
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

// Run starts the chat TUI with the given parameters.
// This is the primary entry point for running the chat interface.
func Run(ctx context.Context, params Params) error {
	model := NewModel(params)
	model.ctx = ctx

	// Ensure chatModeRef is initialized with the model's initial state
	if params.ChatModeRef != nil {
		params.ChatModeRef.SetMode(model.chatMode)
	}

	// Ensure yoloModeRef is initialized with the model's initial state
	if params.YoloModeRef != nil {
		params.YoloModeRef.SetEnabled(model.yoloMode)
	}

	program := tea.NewProgram(
		model,
		tea.WithAltScreen(),
		tea.WithContext(ctx),
		tea.WithMouseCellMotion(), // Enable mouse wheel (use shift+click for text selection)
	)

	_, err := program.Run()
	if err != nil {
		return fmt.Errorf("running chat program: %w", err)
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

// addQueuedPrompt adds a new queued prompt with current state captured.
func (m *Model) addQueuedPrompt(content string) {
	m.queuedPrompts = append(m.queuedPrompts, pendingPrompt{
		content:         content,
		chatMode:        m.chatMode,
		model:           m.currentModel,
		reasoningEffort: m.getReasoningEffort(),
	})
}

// addSteeringPrompt adds a new steering prompt with current state captured.
func (m *Model) addSteeringPrompt(content string) {
	m.steeringPrompts = append(m.steeringPrompts, pendingPrompt{
		content:         content,
		chatMode:        m.chatMode,
		model:           m.currentModel,
		reasoningEffort: m.getReasoningEffort(),
	})
}

// getReasoningEffort returns the current reasoning effort setting.
func (m *Model) getReasoningEffort() string {
	if m.sessionConfig == nil || m.sessionConfig.ReasoningEffort == "" {
		return ""
	}

	return m.sessionConfig.ReasoningEffort
}

// hasPendingPrompts returns true if there are any queued or steering prompts.
func (m *Model) hasPendingPrompts() bool {
	return len(m.queuedPrompts) > 0 || len(m.steeringPrompts) > 0
}

// pendingPromptCount returns the total number of pending prompts.
func (m *Model) pendingPromptCount() int {
	return len(m.queuedPrompts) + len(m.steeringPrompts)
}

// deleteLastPendingPrompt removes the most recently added pending prompt.
// Queued prompts are removed first (most recent), then steering prompts.
// Returns true if a prompt was deleted.
func (m *Model) deleteLastPendingPrompt() bool {
	if len(m.queuedPrompts) > 0 {
		m.queuedPrompts = m.queuedPrompts[:len(m.queuedPrompts)-1]

		return true
	}

	if len(m.steeringPrompts) > 0 {
		m.steeringPrompts = m.steeringPrompts[:len(m.steeringPrompts)-1]

		return true
	}

	return false
}

// popNextPendingPrompt removes and returns the next pending prompt.
// Returns nil if no prompts are pending.
func (m *Model) popNextPendingPrompt() *pendingPrompt {
	if len(m.steeringPrompts) > 0 {
		prompt := m.steeringPrompts[0]
		m.steeringPrompts = m.steeringPrompts[1:]

		return &prompt
	}

	if len(m.queuedPrompts) > 0 {
		prompt := m.queuedPrompts[0]
		m.queuedPrompts = m.queuedPrompts[1:]

		return &prompt
	}

	return nil
}
