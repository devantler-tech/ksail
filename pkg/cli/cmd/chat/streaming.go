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
	closed      bool
}

// markDone signals that streaming is complete.
func (s *streamingState) markDone() {
	if !s.closed {
		s.closed = true
		close(s.done)
	}
}

// handleStreamingEvent processes a single streaming session event.
func handleStreamingEvent(
	event copilot.SessionEvent,
	writer io.Writer,
	state *streamingState,
) {
	state.mu.Lock()
	defer state.mu.Unlock()

	if state.closed {
		return
	}

	//nolint:exhaustive // Only a subset of ~30 SDK event types are relevant for streaming display.
	switch event.Type {
	case copilot.AssistantMessageDelta:
		if event.Data.DeltaContent != nil {
			_, _ = fmt.Fprint(writer, *event.Data.DeltaContent)
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

		_, _ = fmt.Fprintf(writer, "\n🔧 Running: %s%s\n", toolName, toolArgs)
	case copilot.ToolExecutionComplete:
		_, _ = fmt.Fprint(writer, "✓ Done\n")
	default:
		// Ignore other event types
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
		return fmt.Errorf("streaming cancelled: %w", ctx.Err())
	case <-time.After(timeout):
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
