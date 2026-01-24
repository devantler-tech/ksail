package chat

// streamChunkMsg carries a streamed response chunk from the Copilot API.
type streamChunkMsg struct {
	content string
}

// streamEndMsg signals the end of a streamed response.
type streamEndMsg struct{}

// turnEndMsg signals the end of an assistant turn (may not be final).
// Used for AssistantTurnEnd events which fire after each turn, including
// intermediate turns where the assistant calls tools.
type turnEndMsg struct{}

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

// unsubscribeMsg carries the unsubscribe function from the event subscription.
type unsubscribeMsg struct {
	fn func()
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
