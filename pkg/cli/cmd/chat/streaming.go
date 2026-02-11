package chat

import (
	"context"
	"fmt"
	"io"
	"sync"
	"time"

	copilot "github.com/github/copilot-sdk/go"
)

// streamingState manages the state of a streaming chat response.
type streamingState struct {
	done        chan struct{}
	responseErr error
	mu          sync.Mutex
	doneOnce    sync.Once
}

// markDone signals that streaming is complete.
// Safe for concurrent callers via sync.Once.
func (s *streamingState) markDone() {
	s.doneOnce.Do(func() { close(s.done) })
}

// streamingAction describes what I/O to perform after releasing the lock.
type streamingAction int

const (
	actionNone         streamingAction = iota
	actionDelta                        // write delta content
	actionToolStart                    // write tool execution start
	actionToolComplete                 // write tool completion
)

// streamingOutput holds the data needed for post-unlock I/O.
type streamingOutput struct {
	action streamingAction
	text   string
}

// handleStreamingEvent processes a single streaming session event.
// State mutation happens under the lock; I/O happens after unlocking.
func handleStreamingEvent(
	event copilot.SessionEvent,
	writer io.Writer,
	state *streamingState,
) {
	output := computeStreamingOutput(event, state)
	writeStreamingOutput(output, writer)
}

// computeStreamingOutput processes state changes under the lock and returns
// the I/O action to perform after unlocking.
func computeStreamingOutput(event copilot.SessionEvent, state *streamingState) streamingOutput {
	state.mu.Lock()
	defer state.mu.Unlock()

	//nolint:exhaustive // Only a subset of ~30 SDK event types are relevant for streaming display.
	switch event.Type {
	case copilot.AssistantMessageDelta:
		if event.Data.DeltaContent != nil {
			return streamingOutput{action: actionDelta, text: *event.Data.DeltaContent}
		}
	case copilot.SessionIdle:
		state.markDone()
	case copilot.SessionError:
		if event.Data.Message != nil {
			state.responseErr = fmt.Errorf("%w: %s", errSessionError, *event.Data.Message)
		}

		state.markDone()
	case copilot.ToolExecutionStart:
		toolName := getToolName(event)
		toolArgs := getToolArgs(event)

		return streamingOutput{
			action: actionToolStart,
			text:   fmt.Sprintf("\n🔧 Running: %s%s\n", toolName, toolArgs),
		}
	case copilot.ToolExecutionComplete:
		return streamingOutput{action: actionToolComplete}
	default:
		// Ignore other event types
	}

	return streamingOutput{action: actionNone}
}

// writeStreamingOutput performs the I/O operation outside the critical section.
func writeStreamingOutput(output streamingOutput, writer io.Writer) {
	switch output.action {
	case actionDelta:
		_, _ = fmt.Fprint(writer, output.text)
	case actionToolStart:
		_, _ = fmt.Fprint(writer, output.text)
	case actionToolComplete:
		_, _ = fmt.Fprint(writer, "✓ Done\n")
	case actionNone:
		// Nothing to write
	}
}

// sendChatWithStreaming sends a message and streams the response.
// It respects the context for cancellation and the timeout for maximum response time.
func sendChatWithStreaming(
	ctx context.Context,
	session *copilot.Session,
	input string,
	timeout time.Duration,
	writer io.Writer,
) error {
	state := &streamingState{done: make(chan struct{})}

	unsubscribe := session.On(func(event copilot.SessionEvent) {
		handleStreamingEvent(event, writer, state)
	})
	defer unsubscribe()

	_, err := session.Send(copilot.MessageOptions{Prompt: input})
	if err != nil {
		return fmt.Errorf("failed to send chat message: %w", err)
	}

	select {
	case <-state.done:
	case <-ctx.Done():
		_ = session.Abort()

		return fmt.Errorf("streaming cancelled: %w", ctx.Err())
	case <-time.After(timeout):
		_ = session.Abort()

		return fmt.Errorf("%w after %v", errResponseTimeout, timeout)
	}

	return state.responseErr
}

// sendChatWithoutStreaming sends a message and waits for the complete response.
func sendChatWithoutStreaming(
	ctx context.Context,
	session *copilot.Session,
	input string,
	timeout time.Duration,
	writer io.Writer,
) error {
	// Use a channel to make the blocking call cancellable
	type result struct {
		response *copilot.SessionEvent
		err      error
	}

	resultChan := make(chan result, 1)

	go func() {
		response, err := session.SendAndWait(copilot.MessageOptions{Prompt: input}, timeout)
		resultChan <- result{response: response, err: err}
	}()

	select {
	case <-ctx.Done():
		return fmt.Errorf("chat cancelled: %w", ctx.Err())
	case chatResult := <-resultChan:
		if chatResult.err != nil {
			return fmt.Errorf("failed to send chat message: %w", chatResult.err)
		}

		if chatResult.response != nil && chatResult.response.Data.Content != nil {
			_, _ = fmt.Fprintln(writer, *chatResult.response.Data.Content)
		}

		return nil
	}
}
