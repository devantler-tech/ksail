package chat

// streamMsg is a marker interface implemented by every per-turn stream event
// message routed through handleStreamEvent. Model.Update matches the whole
// family with a single `case streamMsg:` so the outer dispatch can never drift
// from the inner handleStreamEvent switch: a new stream message type only has
// to implement isStreamMsg() to be routed correctly, instead of being
// enumerated in two parallel switches (where omitting it from the outer list
// silently fell through to updateSubcomponents).
type streamMsg interface {
	isStreamMsg()
}

// The following no-op methods register each message type as a streamMsg.
// Keep this list in sync with the cases in handleStreamEvent.

func (streamChunkMsg) isStreamMsg()             {}
func (assistantMessageMsg) isStreamMsg()        {}
func (toolStartMsg) isStreamMsg()               {}
func (toolEndMsg) isStreamMsg()                 {}
func (ToolOutputChunkMsg) isStreamMsg()         {}
func (permissionRequestMsg) isStreamMsg()       {}
func (elicitationRequestMsg) isStreamMsg()      {}
func (streamEndMsg) isStreamMsg()               {}
func (turnStartMsg) isStreamMsg()               {}
func (turnEndMsg) isStreamMsg()                 {}
func (reasoningMsg) isStreamMsg()               {}
func (abortMsg) isStreamMsg()                   {}
func (snapshotRewindMsg) isStreamMsg()          {}
func (streamErrMsg) isStreamMsg()               {}
func (usageMsg) isStreamMsg()                   {}
func (compactionStartMsg) isStreamMsg()         {}
func (compactionCompleteMsg) isStreamMsg()      {}
func (intentMsg) isStreamMsg()                  {}
func (modelChangeMsg) isStreamMsg()             {}
func (shutdownMsg) isStreamMsg()                {}
func (systemNotificationMsg) isStreamMsg()      {}
func (sessionWarningMsg) isStreamMsg()          {}
func (ToolProgressMsg) isStreamMsg()            {}
func (TaskCompleteMsg) isStreamMsg()            {}
func (autoModeSwitchRequestedMsg) isStreamMsg() {}
func (autoModeSwitchCompletedMsg) isStreamMsg() {}
