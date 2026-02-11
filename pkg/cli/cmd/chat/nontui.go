package chat

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/notify"
	chatsvc "github.com/devantler-tech/ksail/v5/pkg/svc/chat"
	copilot "github.com/github/copilot-sdk/go"
	"github.com/spf13/cobra"
)

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
	// Set up tools without streaming
	tools, toolMetadata := chatsvc.GetKSailToolMetadata(cmd.Root(), nil) //nolint:contextcheck
	// In non-TUI mode, pass nil for eventChan, agentModeRef, and yoloModeRef:
	// - nil eventChan: tool execution is not streamed to UI (output goes directly to LLM)
	// - nil agentModeRef: agent mode is always enabled (no plan-only mode in non-TUI)
	// - nil yoloModeRef: YOLO mode is not available in non-TUI mode
	tools = WrapToolsWithPermissionAndModeMetadata(tools, nil, nil, nil, toolMetadata)
	sessionConfig.Tools = tools

	// Set up permission handler for non-KSail tools (git, shell, etc.)
	sessionConfig.OnPermissionRequest = chatsvc.CreatePermissionHandler(writer)

	// Create session
	session, err := client.CreateSession(sessionConfig)
	if err != nil {
		return fmt.Errorf("failed to create chat session: %w", err)
	}

	defer func() {
		select {
		case <-ctx.Done():
			os.Exit(signalExitCode)
		default:
			_ = session.Destroy()
		}
	}()

	notify.WriteMessage(notify.Message{
		Type:    notify.SuccessType,
		Content: "Chat session started. Type 'exit' or 'quit' to end the session.",
		Writer:  writer,
	})
	_, _ = fmt.Fprintln(writer, "")

	return runChatInteractiveLoop(ctx, session, flags.streaming, flags.timeout, writer)
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
	writer io.Writer,
) error {
	reader := bufio.NewReader(os.Stdin)
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
