package cmd

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	copilot "github.com/github/copilot-sdk/go"

	runtime "github.com/devantler-tech/ksail/v5/pkg/di"
	chatsvc "github.com/devantler-tech/ksail/v5/pkg/svc/chat"
	"github.com/devantler-tech/ksail/v5/pkg/utils/notify"
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

	cmd.RunE = func(cmd *cobra.Command, _ []string) error {
		return handleChatRunE(cmd)
	}

	return cmd
}

// handleChatRunE handles the chat command execution.
func handleChatRunE(cmd *cobra.Command) error {
	writer := cmd.OutOrStdout()

	notify.WriteMessage(notify.Message{
		Type:    notify.TitleType,
		Content: "Starting KSail AI Assistant...",
		Emoji:   "🤖",
		Writer:  writer,
	})

	// Get flags
	model, _ := cmd.Flags().GetString("model")
	streaming, _ := cmd.Flags().GetBool("streaming")

	// Create Copilot client
	client := copilot.NewClient(&copilot.ClientOptions{
		LogLevel: "error",
	})

	err := client.Start()
	if err != nil {
		return fmt.Errorf("failed to start Copilot client: %w\n\nEnsure GitHub Copilot CLI is installed and COPILOT_CLI_PATH is set", err)
	}
	defer func() {
		_ = client.Stop()
	}()

	// Check authentication
	authStatus, err := client.GetAuthStatus()
	if err != nil {
		return fmt.Errorf("failed to check authentication: %w", err)
	}
	if !authStatus.IsAuthenticated {
		return fmt.Errorf("not authenticated with GitHub Copilot. Run 'copilot auth login' to authenticate")
	}

	loginName := "unknown"
	if authStatus.Login != nil {
		loginName = *authStatus.Login
	}
	notify.WriteMessage(notify.Message{
		Type:    notify.InfoType,
		Content: fmt.Sprintf("Authenticated as %s", loginName),
		Writer:  writer,
	})

	// Build system context from KSail documentation
	systemContext, err := chatsvc.BuildSystemContext()
	if err != nil {
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
		Tools:               chatsvc.GetKSailTools(),
		OnPermissionRequest: chatsvc.CreatePermissionHandler(writer),
	}

	if model != "" {
		sessionConfig.Model = model
	}

	// Create session
	session, err := client.CreateSession(sessionConfig)
	if err != nil {
		return fmt.Errorf("failed to create chat session: %w", err)
	}
	defer func() {
		_ = session.Destroy()
	}()

	notify.WriteMessage(notify.Message{
		Type:    notify.SuccessType,
		Content: "Chat session started. Type 'exit' or 'quit' to end the session.",
		Writer:  writer,
	})

	fmt.Fprintln(writer, "")

	// Start interactive loop
	return runChatInteractiveLoop(session, streaming, writer)
}

// runChatInteractiveLoop runs the interactive chat loop.
func runChatInteractiveLoop(session *copilot.Session, streaming bool, writer io.Writer) error {
	reader := bufio.NewReader(os.Stdin)

	for {
		// Prompt
		fmt.Fprint(writer, "You: ")

		// Read input
		input, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("failed to read input: %w", err)
		}

		input = strings.TrimSpace(input)
		if input == "" {
			continue
		}

		// Check for exit commands
		if isChatExitCommand(input) {
			notify.WriteMessage(notify.Message{
				Type:    notify.InfoType,
				Content: "Chat session ended. Goodbye!",
				Writer:  writer,
			})
			return nil
		}

		// Send message and handle response
		fmt.Fprint(writer, "\nAssistant: ")

		if streaming {
			err = sendChatWithStreaming(session, input, writer)
		} else {
			err = sendChatWithoutStreaming(session, input, writer)
		}

		if err != nil {
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
func sendChatWithStreaming(session *copilot.Session, input string, writer io.Writer) error {
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
			toolName := getChatToolName(event)
			toolArgs := getChatToolArgs(event)
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

	// Wait for completion with timeout
	select {
	case <-done:
	case <-time.After(5 * time.Minute):
		return fmt.Errorf("response timeout")
	}

	return responseErr
}

// sendChatWithoutStreaming sends a message and waits for the complete response.
func sendChatWithoutStreaming(session *copilot.Session, input string, writer io.Writer) error {
	response, err := session.SendAndWait(copilot.MessageOptions{Prompt: input}, 5*time.Minute)
	if err != nil {
		return err
	}

	if response != nil && response.Data.Content != nil {
		fmt.Fprintln(writer, *response.Data.Content)
	}

	return nil
}

// isChatExitCommand checks if the input is an exit command.
func isChatExitCommand(input string) bool {
	lower := strings.ToLower(input)
	return lower == "exit" || lower == "quit" || lower == "q" || lower == "/exit" || lower == "/quit"
}

// getChatToolName extracts the tool name from a session event.
func getChatToolName(event copilot.SessionEvent) string {
	if event.Data.ToolName != nil {
		return *event.Data.ToolName
	}
	return "unknown"
}

// getChatToolArgs formats tool arguments for display.
func getChatToolArgs(event copilot.SessionEvent) string {
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
