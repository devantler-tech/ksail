package chat

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	chatui "github.com/devantler-tech/ksail/v7/pkg/cli/ui/chat"
	"github.com/devantler-tech/ksail/v7/pkg/notify"
	chatsvc "github.com/devantler-tech/ksail/v7/pkg/svc/chat"
	"github.com/devantler-tech/ksail/v7/pkg/toolgen"
	copilot "github.com/github/copilot-sdk/go"
	"github.com/spf13/cobra"
)

// Sentinel errors for the non-TUI chat loop.
var (
	errResponseTimeout = errors.New("response timeout")
	errSessionError    = errors.New("session error")
)

// notifyNonTUIStartup sends startup notifications when running outside the TUI.
func notifyNonTUIStartup(writer io.Writer) {
	notify.WriteMessage(notify.Message{
		Type:    notify.TitleType,
		Content: "Starting KSail AI Assistant...",
		Emoji:   "🤖",
		Writer:  writer,
	})
}

// setupNonTUISignalHandler configures signal handling for non-TUI mode.
func setupNonTUISignalHandler(
	cancel context.CancelFunc,
	writer io.Writer,
) {
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigChan
		notify.WriteMessage(notify.Message{
			Type:    notify.InfoType,
			Content: "\nReceived interrupt signal, shutting down...",
			Writer:  writer,
		})
		cancel()
		time.Sleep(signalSleepDuration)
		os.Exit(signalExitCode)
	}()
}

// runNonTUIChat handles the non-TUI chat mode.
func runNonTUIChat(
	ctx context.Context,
	client *copilot.Client,
	sessionConfig *copilot.SessionConfig,
	flags flags,
	cmd *cobra.Command,
	writer io.Writer,
) error {
	// Create session log ref for SDK-native tool logging
	sessionLog := toolgen.NewSessionLogRef()

	// Set up tools without streaming
	tools, toolMetadata := chatsvc.GetKSailToolMetadata(
		cmd.Root(), nil, sessionLog,
	)
	tools = WrapToolsWithForceInjection(tools, toolMetadata)
	sessionConfig.Tools = tools

	// Set up permission handler for non-KSail tools (git, shell, etc.)
	sessionConfig.OnPermissionRequest = chatsvc.CreatePermissionHandler(writer)

	// Set up pre-tool-use hook for path sandboxing
	allowedRoot, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to determine working directory for sandboxing: %w", err)
	}

	sessionConfig.Hooks = &copilot.SessionHooks{
		OnPreToolUse: BuildPreToolUseHook(allowedRoot),
	}

	// Register OnEvent handler for session-level events not in the per-turn handler
	sessionConfig.OnEvent = buildNonTUIOnEventHandler(writer)

	// Register slash commands for non-TUI mode
	sessionConfig.Commands = chatui.BuildNonTUISlashCommands(writer)

	// Create shared stdin reader for both the interactive loop and elicitation handler.
	// Using a single bufio.Reader prevents data loss from buffered reads.
	stdinReader := bufio.NewReader(os.Stdin)

	// Register elicitation handler for MCP tool form requests
	sessionConfig.OnElicitationRequest = chatsvc.CreateElicitationHandler(stdinReader, writer)

	// Create session
	session, err := client.CreateSession(ctx, sessionConfig)
	if err != nil {
		return fmt.Errorf("failed to create chat session: %w", err)
	}

	// Wire session log now that the session exists
	wireSessionLog(session, sessionLog)

	defer func() {
		select {
		case <-ctx.Done():
			os.Exit(signalExitCode)
		default:
			_ = session.Disconnect()
		}
	}()

	notify.WriteMessage(notify.Message{
		Type:    notify.SuccessType,
		Content: "Chat session started. Type 'exit' or 'quit' to end the session.",
		Writer:  writer,
	})
	_, _ = fmt.Fprintln(writer, "")

	return runChatInteractiveLoop(ctx, session, flags.streaming, flags.timeout, stdinReader, writer)
}

// inputResult holds the result of reading from stdin.
type inputResult struct {
	input string
	err   error
}

// readUserInput prompts for and reads user input, supporting context cancellation.
// Returns the trimmed input string, or an error if reading fails or context is cancelled.
// Returns io.EOF when the input stream ends (e.g., piped input).
//
// NOTE: The stdin reading goroutine cannot be interrupted once started, as Go's
// bufio.Reader.ReadString blocks until input or EOF. If context is cancelled
// before input arrives, one goroutine will remain blocked until process exit.
// This is a known Go limitation with blocking stdin reads.
func readUserInput(
	ctx context.Context,
	reader *bufio.Reader,
	inputChan chan inputResult,
	writer io.Writer,
) (string, error) {
	_, _ = fmt.Fprint(writer, "You: ")

	go func() {
		input, readErr := reader.ReadString('\n')
		inputChan <- inputResult{input: input, err: readErr}
	}()

	select {
	case <-ctx.Done():
		return "", fmt.Errorf("input cancelled: %w", ctx.Err())
	case result := <-inputChan:
		if result.err != nil {
			if errors.Is(result.err, io.EOF) {
				return "", io.EOF
			}

			return "", fmt.Errorf("failed to read input: %w", result.err)
		}

		return strings.TrimSpace(result.input), nil
	}
}

// sendAndDisplayResponse sends a chat message and displays the response.
func sendAndDisplayResponse(
	ctx context.Context,
	session *copilot.Session,
	input string,
	streaming bool,
	timeout time.Duration,
	writer io.Writer,
) error {
	_, _ = fmt.Fprint(writer, "\nAssistant: ")

	var sendErr error
	if streaming {
		sendErr = sendChatWithStreaming(ctx, session, input, timeout, writer)
	} else {
		sendErr = sendChatWithoutStreaming(ctx, session, input, timeout, writer)
	}

	if sendErr != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("chat interrupted: %w", ctx.Err())
		}

		notify.WriteMessage(notify.Message{
			Type:    notify.ErrorType,
			Content: "Error: " + sendErr.Error(),
			Writer:  writer,
		})
	}

	_, _ = fmt.Fprintln(writer, "")

	return nil
}

// runChatInteractiveLoop runs the interactive chat loop.
// It handles user input and AI responses until the user exits or the context is cancelled.
func runChatInteractiveLoop(
	ctx context.Context,
	session *copilot.Session,
	streaming bool,
	timeout time.Duration,
	reader *bufio.Reader,
	writer io.Writer,
) error {
	inputChan := make(chan inputResult, 1)

	for {
		input, err := readUserInput(ctx, reader, inputChan, writer)
		if errors.Is(err, io.EOF) || ctx.Err() != nil {
			//nolint:nilerr // EOF and context cancellation are graceful exit conditions, not errors.
			return nil
		}

		if err != nil {
			return err
		}

		if input == "" {
			continue
		}

		if isExitCommand(input) {
			notify.WriteMessage(notify.Message{
				Type:    notify.InfoType,
				Content: "Chat session ended. Goodbye!",
				Writer:  writer,
			})

			return nil
		}

		sendErr := sendAndDisplayResponse(
			ctx, session, input, streaming, timeout, writer,
		)
		if sendErr != nil {
			return sendErr
		}
	}
}

// isExitCommand checks if the input is an exit command.
func isExitCommand(input string) bool {
	lower := strings.ToLower(input)

	return lower == "exit" || lower == "quit" || lower == "q" || lower == "/exit" ||
		lower == "/quit"
}

// buildNonTUIOnEventHandler creates an OnEvent handler for non-TUI mode that logs
// session-level events not covered by the per-turn streaming handler.
func buildNonTUIOnEventHandler(writer io.Writer) copilot.SessionEventHandler {
	return func(event copilot.SessionEvent) {
		//nolint:exhaustive // Only session-level events not in per-turn handler; rest ignored.
		switch event.Type() {
		case copilot.SessionEventTypeToolExecutionProgress:
			if data, ok := event.Data.(*copilot.ToolExecutionProgressData); ok {
				_, _ = fmt.Fprintf(writer, "  ⏳ %s\n", data.ProgressMessage)
			}
		case copilot.SessionEventTypeSessionTaskComplete:
			data, isTaskComplete := event.Data.(*copilot.SessionTaskCompleteData)
			if isTaskComplete && data.Summary != nil {
				_, _ = fmt.Fprintf(writer, "\n✅ %s\n", *data.Summary)
			}
		}
	}
}

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
//
//nolint:cyclop // type-switch dispatcher for session events
func computeStreamingOutput(event copilot.SessionEvent, state *streamingState) streamingOutput {
	state.mu.Lock()
	defer state.mu.Unlock()

	//nolint:exhaustive // Only a subset of ~30 SDK event types are relevant for streaming display.
	switch event.Type() {
	case copilot.SessionEventTypeAssistantMessageDelta:
		if data, ok := event.Data.(*copilot.AssistantMessageDeltaData); ok {
			return streamingOutput{action: actionDelta, text: data.DeltaContent}
		}
	case copilot.SessionEventTypeSessionIdle:
		state.markDone()
	case copilot.SessionEventTypeSessionError:
		if data, ok := event.Data.(*copilot.SessionErrorData); ok {
			state.responseErr = fmt.Errorf("%w: %s", errSessionError, data.Message)
		}

		state.markDone()
	case copilot.SessionEventTypeToolExecutionStart:
		toolName := getToolName(event)
		toolArgs := getToolArgs(event)

		return streamingOutput{
			action: actionToolStart,
			text:   fmt.Sprintf("\n🔧 Running: %s%s\n", toolName, toolArgs),
		}
	case copilot.SessionEventTypeToolExecutionComplete:
		return streamingOutput{action: actionToolComplete}
	case copilot.SessionEventTypeSystemNotification:
		if data, ok := event.Data.(*copilot.SystemNotificationData); ok {
			return streamingOutput{action: actionDelta, text: "\nℹ️ " + data.Content + "\n"}
		}
	case copilot.SessionEventTypeSessionWarning:
		if data, ok := event.Data.(*copilot.SessionWarningData); ok {
			return streamingOutput{action: actionDelta, text: "\n⚠️ " + data.Message + "\n"}
		}
	case copilot.SessionEventTypeToolExecutionProgress:
		if data, ok := event.Data.(*copilot.ToolExecutionProgressData); ok {
			return streamingOutput{
				action: actionDelta,
				text:   "  ⏳ " + data.ProgressMessage + "\n",
			}
		}
	case copilot.SessionEventTypeSessionTaskComplete:
		if data, ok := event.Data.(*copilot.SessionTaskCompleteData); ok && data.Summary != nil {
			return streamingOutput{action: actionDelta, text: "\n✅ " + *data.Summary + "\n"}
		}
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

	_, err := session.Send(ctx, copilot.MessageOptions{Prompt: input})
	if err != nil {
		return fmt.Errorf("failed to send chat message: %w", err)
	}

	select {
	case <-state.done:
	case <-ctx.Done():
		_ = session.Abort(ctx)

		return fmt.Errorf("streaming cancelled: %w", ctx.Err())
	case <-time.After(timeout):
		_ = session.Abort(ctx)

		return fmt.Errorf("%w after %v", errResponseTimeout, timeout)
	}

	return state.responseErr
}

// sendChatWithoutStreaming sends a message and waits for the complete response.
// The timeout is enforced via a derived context so that SendAndWait (which uses
// context-based cancellation) is bounded in time.
func sendChatWithoutStreaming(
	ctx context.Context,
	session *copilot.Session,
	input string,
	timeout time.Duration,
	writer io.Writer,
) error {
	// Wrap the parent context with a timeout so SendAndWait respects the deadline.
	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Use a channel to make the blocking call cancellable
	type result struct {
		response *copilot.SessionEvent
		err      error
	}

	resultChan := make(chan result, 1)

	go func() {
		response, err := session.SendAndWait(timeoutCtx, copilot.MessageOptions{Prompt: input})
		resultChan <- result{response: response, err: err}
	}()

	select {
	case <-timeoutCtx.Done():
		// Abort the in-flight Copilot request when the context is cancelled or timed out.
		_ = session.Abort(ctx)

		if ctx.Err() != nil {
			return fmt.Errorf("chat cancelled: %w", ctx.Err())
		}

		return fmt.Errorf("%w after %v", errResponseTimeout, timeout)
	case chatResult := <-resultChan:
		if chatResult.err != nil {
			return fmt.Errorf("failed to send chat message: %w", chatResult.err)
		}

		if chatResult.response != nil {
			if data, ok := chatResult.response.Data.(*copilot.AssistantMessageData); ok {
				_, _ = fmt.Fprintln(writer, data.Content)
			}
		}

		return nil
	}
}

// getToolName extracts the tool name from a session event.
func getToolName(event copilot.SessionEvent) string {
	if data, ok := event.Data.(*copilot.ToolExecutionStartData); ok {
		return data.ToolName
	}

	return "unknown"
}

// formatArgsMap converts a map of arguments to a comma-separated key=value string.
// Keys are sorted for consistent output across runs.
func formatArgsMap(args map[string]any) string {
	if len(args) == 0 {
		return ""
	}

	// Sort keys for consistent output (Go map iteration order is non-deterministic)
	keys := make([]string, 0, len(args))
	for k := range args {
		keys = append(keys, k)
	}

	slices.Sort(keys)

	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%v", k, args[k]))
	}

	return strings.Join(parts, ", ")
}

// getToolArgs formats tool arguments for display with parentheses.
func getToolArgs(event copilot.SessionEvent) string {
	startData, isStartData := event.Data.(*copilot.ToolExecutionStartData)
	if !isStartData || startData.Arguments == nil {
		return ""
	}

	args, isMap := startData.Arguments.(map[string]any)
	if !isMap || len(args) == 0 {
		return ""
	}

	formatted := formatArgsMap(args)
	if formatted == "" {
		return ""
	}

	return " (" + formatted + ")"
}
