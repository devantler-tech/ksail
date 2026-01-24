package chat

// streamChunkMsg carries a streamed response chunk from the Copilot API.
type streamChunkMsg struct {
	content string
}

// streamEndMsg signals the end of a streamed response.
type streamEndMsg struct{}

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

// unsubscribeMsg carries the unsubscribe function from the event subscription.
type unsubscribeMsg struct {
	fn func()
}

// permissionRequestMsg signals that a tool requires user approval before execution.
type permissionRequestMsg struct {
	toolName    string
	command     string
	description string
	respondChan chan<- bool // Channel to send the user's response
}
