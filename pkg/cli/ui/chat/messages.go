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
	toolName string
}

// toolEndMsg signals the completion of a tool execution with its output.
type toolEndMsg struct {
	toolName string
	output   string
}

// unsubscribeMsg carries the unsubscribe function from the event subscription.
type unsubscribeMsg struct {
	fn func()
}
