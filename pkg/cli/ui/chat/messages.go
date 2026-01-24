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
	toolID   string
	toolName string
	command  string // The actual command being executed (e.g., "ksail cluster list --all")
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

// permissionRequestMsg signals that a tool requires user approval before execution.
type permissionRequestMsg struct {
	toolName    string
	command     string
	args        []string
	path        string
	content     string
	description string
	respondChan chan<- bool // Channel to send the user's response
}
