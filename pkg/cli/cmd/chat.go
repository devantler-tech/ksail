package cmd

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	chatui "github.com/devantler-tech/ksail/v5/pkg/cli/ui/chat"
	runtime "github.com/devantler-tech/ksail/v5/pkg/di"
	chatsvc "github.com/devantler-tech/ksail/v5/pkg/svc/chat"
	"github.com/devantler-tech/ksail/v5/pkg/svc/chat/generator"
	"github.com/devantler-tech/ksail/v5/pkg/utils/notify"
	copilot "github.com/github/copilot-sdk/go"
	"github.com/spf13/cobra"
)

// NewChatCmd creates and returns the chat command.
func NewChatCmd(_ *runtime.Runtime) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "chat",
		Short: "Start an AI-assisted chat session",
		Long: `Start an interactive AI chat session powered by GitHub Copilot.

The assistant understands KSail's CLI, configuration schemas, and can help with:
  - Guided cluster configuration and setup
  - Troubleshooting cluster issues
  - Explaining KSail concepts and features
  - Running KSail commands with your approval

Prerequisites:
  - GitHub Copilot CLI must be installed and authenticated
  - Set COPILOT_CLI_PATH if the CLI is not in your PATH

Write operations require explicit confirmation before execution.`,
		SilenceUsage: true,
	}

	// Optional flags
	cmd.Flags().StringP("model", "m", "", "Model to use (e.g., gpt-5, claude-sonnet-4)")
	cmd.Flags().BoolP("streaming", "s", true, "Enable streaming responses")
	cmd.Flags().DurationP("timeout", "t", 5*time.Minute, "Response timeout duration")
	cmd.Flags().Bool("tui", true, "Use interactive TUI mode with markdown rendering")

	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		return handleChatRunE(cmd)
	}

	return cmd
}

// handleChatRunE handles the chat command execution.
func handleChatRunE(cmd *cobra.Command) error {
	writer := cmd.OutOrStdout()

	// Set up signal handling for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Get flags early to determine mode
	model, _ := cmd.Flags().GetString("model")
	streaming, _ := cmd.Flags().GetBool("streaming")
	timeout, _ := cmd.Flags().GetDuration("timeout")
	useTUI, _ := cmd.Flags().GetBool("tui")

	// Set up signal handler - TUI handles its own signals
	if !useTUI {
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
			// Force exit after a brief delay to allow message to print
			time.Sleep(50 * time.Millisecond)
			os.Exit(130) // Standard exit code for Ctrl+C
		}()

		notify.WriteMessage(notify.Message{
			Type:    notify.TitleType,
			Content: "Starting KSail AI Assistant...",
			Emoji:   "🤖",
			Writer:  writer,
		})
	}

	// Create Copilot client
	client := copilot.NewClient(&copilot.ClientOptions{
		LogLevel: "error",
	})

	err := client.Start()
	if err != nil {
		return fmt.Errorf(
			"failed to start Copilot client: %w\n\n"+
				"To fix:\n"+
				"  1. Install GitHub Copilot CLI: npm install -g @githubnext/github-copilot-cli\n"+
				"  2. Or set COPILOT_CLI_PATH to your installation",
			err,
		)
	}
	// Cleanup with forced exit on interrupt to prevent hanging
	defer func() {
		select {
		case <-ctx.Done():
			// If interrupted, force exit without waiting for cleanup
			os.Exit(130) // Standard exit code for Ctrl+C
		default:
			_ = client.Stop()
		}
	}()

	// Check authentication
	authStatus, err := client.GetAuthStatus()
	if err != nil {
		return fmt.Errorf("failed to check authentication: %w", err)
	}
	if !authStatus.IsAuthenticated {
		return fmt.Errorf(
			"not authenticated with GitHub Copilot\n\n" +
				"To fix:\n" +
				"  1. Run: gh auth login\n" +
				"  2. Ensure you have an active GitHub Copilot subscription",
		)
	}

	loginName := "unknown"
	if authStatus.Login != nil {
		loginName = *authStatus.Login
	}

	if !useTUI {
		notify.WriteMessage(notify.Message{
			Type:    notify.InfoType,
			Content: fmt.Sprintf("Authenticated as %s", loginName),
			Writer:  writer,
		})
	}

	// Build system context from KSail documentation
	systemContext, err := chatsvc.BuildSystemContext()
	if err != nil && !useTUI {
		notify.WriteMessage(notify.Message{
			Type:    notify.WarningType,
			Content: fmt.Sprintf("Could not load full context: %v", err),
			Writer:  writer,
		})
	}

	// Create session configuration
	sessionConfig := &copilot.SessionConfig{
		Streaming: streaming,
		SystemMessage: &copilot.SystemMessageConfig{
			Mode:    "append",
			Content: systemContext,
		},
	}

	if model != "" {
		sessionConfig.Model = model
	}

	// Start interactive loop - TUI or simple mode
	if useTUI {
		return runTUIChat(ctx, client, sessionConfig, timeout, cmd.Root())
	}

	// Non-TUI mode: use standard permission handler and tools without streaming
	sessionConfig.Tools = chatsvc.GetKSailTools(cmd.Root(), nil)
	sessionConfig.OnPermissionRequest = chatsvc.CreatePermissionHandler(writer)

	// Create session
	session, err := client.CreateSession(sessionConfig)
	if err != nil {
		return fmt.Errorf("failed to create chat session: %w", err)
	}
	// Cleanup with forced exit on interrupt to prevent hanging
	defer func() {
		select {
		case <-ctx.Done():
			// If interrupted, force exit without waiting for cleanup
			os.Exit(130) // Standard exit code for Ctrl+C
		default:
			_ = session.Destroy()
		}
	}()

	notify.WriteMessage(notify.Message{
		Type:    notify.SuccessType,
		Content: "Chat session started. Type 'exit' or 'quit' to end the session.",
		Writer:  writer,
	})

	fmt.Fprintln(writer, "")

	return runChatInteractiveLoop(ctx, session, streaming, timeout, writer)
}

// runTUIChat starts the TUI chat mode.
func runTUIChat(
	ctx context.Context,
	client *copilot.Client,
	sessionConfig *copilot.SessionConfig,
	timeout time.Duration,
	rootCmd *cobra.Command,
) error {
	// Create event channel for TUI communication
	eventChan := make(chan tea.Msg, 100)

	// Create output channel for real-time tool output streaming
	outputChan := make(chan generator.OutputChunk, 100)

	// Forward output chunks to the TUI event channel
	go func() {
		for chunk := range outputChan {
			eventChan <- chatui.ToolOutputChunkMsg{
				ToolID: chunk.ToolID,
				Chunk:  chunk.Chunk,
			}
		}
	}()

	// Set up tools with real-time output streaming
	sessionConfig.Tools = chatsvc.GetKSailTools(rootCmd, outputChan)

	// Set up permission handler that integrates with the TUI
	sessionConfig.OnPermissionRequest = chatui.CreateTUIPermissionHandler(eventChan)

	// Create session
	session, err := client.CreateSession(sessionConfig)
	if err != nil {
		return fmt.Errorf("failed to create chat session: %w", err)
	}
	// Cleanup with forced exit on interrupt to prevent hanging
	defer func() {
		close(outputChan) // Close output channel to stop forwarder goroutine
		select {
		case <-ctx.Done():
			// If interrupted, force exit without waiting for cleanup
			os.Exit(130) // Standard exit code for Ctrl+C
		default:
			_ = session.Destroy()
		}
	}()

	return chatui.RunWithEventChannel(ctx, session, timeout, eventChan)
}

// inputResult holds the result of reading from stdin.
type inputResult struct {
	input string
	err   error
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
		// Check for cancellation before prompting
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		// Prompt
		fmt.Fprint(writer, "You: ")

		// Read input in a goroutine so we can respond to context cancellation
		go func() {
			input, err := reader.ReadString('\n')
			inputChan <- inputResult{input: input, err: err}
		}()

		// Wait for either input or cancellation
		var input string
		select {
		case <-ctx.Done():
			return nil
		case result := <-inputChan:
			if result.err != nil {
				if result.err == io.EOF {
					return nil // Graceful exit on EOF (e.g., piped input)
				}
				return fmt.Errorf("failed to read input: %w", result.err)
			}
			input = strings.TrimSpace(result.input)
		}

		if input == "" {
			continue
		}

		// Check for exit commands
		if isExitCommand(input) {
			notify.WriteMessage(notify.Message{
				Type:    notify.InfoType,
				Content: "Chat session ended. Goodbye!",
				Writer:  writer,
			})
			return nil
		}

		// Send message and handle response
		fmt.Fprint(writer, "\nAssistant: ")

		var err error
		if streaming {
			err = sendChatWithStreaming(ctx, session, input, timeout, writer)
		} else {
			err = sendChatWithoutStreaming(ctx, session, input, timeout, writer)
		}

		if err != nil {
			// Check if error was due to context cancellation
			if ctx.Err() != nil {
				return nil
			}
			notify.WriteMessage(notify.Message{
				Type:    notify.ErrorType,
				Content: fmt.Sprintf("Error: %v", err),
				Writer:  writer,
			})
		}

		fmt.Fprintln(writer, "")
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
	done := make(chan struct{})
	var responseErr error
	var mu sync.Mutex
	closed := false

	unsubscribe := session.On(func(event copilot.SessionEvent) {
		mu.Lock()
		defer mu.Unlock()

		if closed {
			return
		}

		switch event.Type {
		case copilot.AssistantMessageDelta:
			if event.Data.DeltaContent != nil {
				fmt.Fprint(writer, *event.Data.DeltaContent)
			}
		case copilot.SessionIdle:
			if !closed {
				closed = true
				close(done)
			}
		case copilot.SessionError:
			if event.Data.Message != nil {
				responseErr = fmt.Errorf("%s", *event.Data.Message)
			}
			if !closed {
				closed = true
				close(done)
			}
		case copilot.ToolExecutionStart:
			toolName := getToolName(event)
			toolArgs := getToolArgs(event)
			fmt.Fprintf(writer, "\n🔧 Running: %s%s\n", toolName, toolArgs)
		case copilot.ToolExecutionComplete:
			fmt.Fprint(writer, "✓ Done\n")
		}
	})
	defer unsubscribe()

	_, err := session.Send(copilot.MessageOptions{Prompt: input})
	if err != nil {
		return err
	}

	// Wait for completion with timeout and context cancellation
	select {
	case <-done:
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(timeout):
		return fmt.Errorf("response timeout after %v", timeout)
	}

	return responseErr
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
		return ctx.Err()
	case r := <-resultChan:
		if r.err != nil {
			return r.err
		}
		if r.response != nil && r.response.Data.Content != nil {
			fmt.Fprintln(writer, *r.response.Data.Content)
		}
		return nil
	}
}

// isExitCommand checks if the input is an exit command.
func isExitCommand(input string) bool {
	lower := strings.ToLower(input)
	return lower == "exit" || lower == "quit" || lower == "q" || lower == "/exit" ||
		lower == "/quit"
}

// getToolName extracts the tool name from a session event.
func getToolName(event copilot.SessionEvent) string {
	if event.Data.ToolName != nil {
		return *event.Data.ToolName
	}
	return "unknown"
}

// getToolArgs formats tool arguments for display.
func getToolArgs(event copilot.SessionEvent) string {
	if event.Data.Arguments == nil {
		return ""
	}
	args, ok := event.Data.Arguments.(map[string]interface{})
	if !ok {
		return ""
	}
	var parts []string
	for k, v := range args {
		parts = append(parts, fmt.Sprintf("%s=%v", k, v))
	}
	if len(parts) == 0 {
		return ""
	}
	return " (" + strings.Join(parts, ", ") + ")"
}
