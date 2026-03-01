package chat

// streamChunkMsg carries a streamed response chunk from the Copilot API.
type streamChunkMsg struct {
	content string
}

// streamEndMsg signals the end of a streamed response.
type streamEndMsg struct{}

// turnStartMsg signals the start of a new assistant turn.
// This fires when the assistant begins processing (after user message or tool results).
type turnStartMsg struct{}

// turnEndMsg signals the end of an assistant turn (may not be final).
// Used for AssistantTurnEnd events which fire after each turn, including
// intermediate turns where the assistant calls tools.
type turnEndMsg struct{}

// reasoningMsg carries reasoning content from the assistant.
// This indicates the LLM is actively "thinking" about the response.
type reasoningMsg struct {
	content string
	isDelta bool // true if this is incremental content
}

// abortMsg signals the session was aborted.
type abortMsg struct{}

// assistantMessageMsg carries the final complete message from the assistant.
// This is sent regardless of streaming mode and contains the full response.
// Per SDK best practices, this is more reliable for completion detection than
// tracking message deltas.
type assistantMessageMsg struct {
	content string
}

// streamErrMsg carries an error encountered during streaming.
type streamErrMsg struct {
	err error
}

// userSubmitMsg signals that the user submitted a message.
type userSubmitMsg struct {
	content string
}

// toolStartMsg signals the start of a tool execution.
type toolStartMsg struct {
	toolID        string
	toolName      string
	command       string // The actual command being executed (e.g., "ksail cluster list --all")
	mcpServerName string // MCP server name (if tool is from MCP server, empty otherwise)
	mcpToolName   string // MCP tool name (if tool is from MCP server, empty otherwise)
}

// toolEndMsg signals the completion of a tool execution with its output.
type toolEndMsg struct {
	toolID   string
	toolName string
	output   string
	success  bool
}

// toolOutputChunkMsg carries a chunk of output from a running tool.
type toolOutputChunkMsg struct {
	toolID string
	chunk  string
}

// ToolOutputChunkMsg is the exported version of toolOutputChunkMsg for external use.
type ToolOutputChunkMsg struct {
	ToolID string
	Chunk  string
}

// permissionRequestMsg carries a permission request from a tool execution.
// The TUI will display this to the user for approval/denial.
type permissionRequestMsg struct {
	toolCallID string      // unique identifier for this tool call
	toolName   string      // name of the tool requesting permission
	command    string      // the actual command or action being requested
	arguments  string      // formatted arguments for display
	response   chan<- bool // channel to send user response (true=allow, false=deny)
}

// PermissionRequestMsg is the exported version of permissionRequestMsg for external use.
type PermissionRequestMsg struct {
	ToolCallID string
	ToolName   string
	Command    string
	Arguments  string
	Response   chan<- bool
}

// copyFeedbackClearMsg signals that the copy feedback should be hidden.
type copyFeedbackClearMsg struct{}

// modelUnavailableClearMsg signals that the model-unavailable feedback should be hidden.
type modelUnavailableClearMsg struct{}

// snapshotRewindMsg signals that the session was rewound to a previous state.
// This can happen when the user discards changes or reverts to a checkpoint.
type snapshotRewindMsg struct{}

// usageMsg carries token usage and billing statistics from the assistant.
type usageMsg struct {
	model          string
	inputTokens    float64
	outputTokens   float64
	cost           float64
	quotaSnapshots map[string]quotaSnapshot
}

// quotaSnapshot mirrors copilot.QuotaSnapshot for internal use,
// avoiding direct SDK type dependency in message passing.
type quotaSnapshot struct {
	entitlementRequests   float64
	isUnlimited           bool
	usedRequests          float64
	remainingPercentage   float64
	resetDate             string // formatted as "Jan 2" for display
	overage               float64
	overageAllowedAtQuota bool
}

// compactionStartMsg signals that context compaction has started.
type compactionStartMsg struct{}

// compactionCompleteMsg signals that context compaction has completed.
type compactionCompleteMsg struct {
	success              bool
	preCompactionTokens  float64
	postCompactionTokens float64
	tokensRemoved        float64
}

// intentMsg carries the assistant's intent/plan for the current turn.
type intentMsg struct {
	content string
}

// modelChangeMsg signals that the session's model was changed server-side.
type modelChangeMsg struct {
	previousModel string
	newModel      string
}

// shutdownMsg signals that the session has ended.
type shutdownMsg struct {
	shutdownType string
}

// warningMsg carries a warning from the session.
type warningMsg struct {
	message string
}

// modeChangedMsg signals a server-side mode change (e.g., autopilot → plan).
type modeChangedMsg struct {
	previousMode string
	newMode      string
}
