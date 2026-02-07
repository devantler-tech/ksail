package cmd

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/devantler-tech/ksail/v5/pkg/ai/toolgen"
	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/cli/annotations"
	chatui "github.com/devantler-tech/ksail/v5/pkg/cli/ui/chat"
	runtime "github.com/devantler-tech/ksail/v5/pkg/di"
	chatsvc "github.com/devantler-tech/ksail/v5/pkg/svc/chat"
	"github.com/devantler-tech/ksail/v5/pkg/utils/notify"
	copilot "github.com/github/copilot-sdk/go"
	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"
)

// chatFlags holds parsed flags for the chat command.
type chatFlags struct {
	model     string
	streaming bool
	timeout   time.Duration
	useTUI    bool
}

// parseChatFlags extracts and resolves chat command flags.
func parseChatFlags(cmd *cobra.Command) chatFlags {
	modelFlag, _ := cmd.Flags().GetString("model")
	streaming, _ := cmd.Flags().GetBool("streaming")
	timeout, _ := cmd.Flags().GetDuration("timeout")
	useTUI, _ := cmd.Flags().GetBool("tui")

	// Determine model: flag > config > "" (auto)
	model := modelFlag
	if model == "" {
		if configModel := loadChatModelFromConfig(); configModel != "" && configModel != "auto" {
			model = configModel
		}
	}

	return chatFlags{
		model:     model,
		streaming: streaming,
		timeout:   timeout,
		useTUI:    useTUI,
	}
}

// startCopilotClient creates and starts a Copilot client.
func startCopilotClient() (*copilot.Client, error) {
	client := copilot.NewClient(&copilot.ClientOptions{
		LogLevel: "error",
	})

	err := client.Start()
	if err != nil {
		return nil, fmt.Errorf(
			"failed to start Copilot client: %w\n\n"+
				"To fix:\n"+
				"  1. Install GitHub Copilot CLI: npm install -g @githubnext/github-copilot-cli\n"+
				"  2. Or set COPILOT_CLI_PATH to your installation",
			err,
		)
	}

	return client, nil
}

// validateCopilotAuth checks authentication and returns the login name.
func validateCopilotAuth(client *copilot.Client) (string, error) {
	authStatus, err := client.GetAuthStatus()
	if err != nil {
		return "", fmt.Errorf("failed to check authentication: %w", err)
	}

	if !authStatus.IsAuthenticated {
		return "", fmt.Errorf(
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

	return loginName, nil
}

// buildSessionConfig creates the Copilot session configuration.
func buildSessionConfig(
	model string,
	streaming bool,
	systemContext string,
) *copilot.SessionConfig {
	config := &copilot.SessionConfig{
		Streaming: streaming,
		SystemMessage: &copilot.SystemMessageConfig{
			Mode:    "append",
			Content: systemContext,
		},
	}

	if model != "" {
		config.Model = model
	}

	return config
}

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
		Annotations: map[string]string{
			annotations.AnnotationExclude: "true",
		},
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
		time.Sleep(50 * time.Millisecond)
		os.Exit(130)
	}()
}

// runNonTUIChat handles the non-TUI chat mode.
func runNonTUIChat(
	ctx context.Context,
	client *copilot.Client,
	sessionConfig *copilot.SessionConfig,
	flags chatFlags,
	cmd *cobra.Command,
	writer io.Writer,
) error {
	// Set up tools without streaming
	tools, toolMetadata := chatsvc.GetKSailToolMetadata(cmd.Root(), nil) //nolint:contextcheck
	// In non-TUI mode, pass nil for eventChan and agentModeRef:
	// - nil eventChan: tool execution is not streamed to UI (output goes directly to LLM)
	// - nil agentModeRef: agent mode is always enabled (no plan-only mode in non-TUI)
	tools = WrapToolsWithPermissionAndModeMetadata(tools, nil, nil, toolMetadata)
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
			os.Exit(130)
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

	return runChatInteractiveLoop(ctx, session, flags.streaming, flags.timeout, writer)
}

// handleChatRunE handles the chat command execution.
func handleChatRunE(cmd *cobra.Command) error {
	writer := cmd.OutOrStdout()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	flags := parseChatFlags(cmd)

	if !flags.useTUI {
		setupNonTUISignalHandler(cancel, writer)
		notify.WriteMessage(notify.Message{
			Type:    notify.TitleType,
			Content: "Starting KSail AI Assistant...",
			Emoji:   "🤖",
			Writer:  writer,
		})
	}

	client, err := startCopilotClient()
	if err != nil {
		return err
	}

	defer func() {
		select {
		case <-ctx.Done():
			os.Exit(130)
		default:
			_ = client.Stop()
		}
	}()

	loginName, err := validateCopilotAuth(client)
	if err != nil {
		return err
	}

	if !flags.useTUI {
		notify.WriteMessage(notify.Message{
			Type:    notify.InfoType,
			Content: fmt.Sprintf("Authenticated as %s", loginName),
			Writer:  writer,
		})
	}

	systemContext, err := chatsvc.BuildSystemContext()
	if err != nil && !flags.useTUI {
		notify.WriteMessage(notify.Message{
			Type:    notify.WarningType,
			Content: fmt.Sprintf("Could not load full context: %v", err),
			Writer:  writer,
		})
	}

	sessionConfig := buildSessionConfig(flags.model, flags.streaming, systemContext)

	if flags.useTUI {
		return runTUIChat(ctx, client, sessionConfig, flags.timeout, cmd.Root())
	}

	return runNonTUIChat(ctx, client, sessionConfig, flags, cmd, writer)
}

// runTUIChat starts the TUI chat mode.
func runTUIChat(
	ctx context.Context,
	client *copilot.Client,
	sessionConfig *copilot.SessionConfig,
	timeout time.Duration,
	rootCmd *cobra.Command,
) error {
	// Fetch available models
	allModels, err := client.ListModels()
	if err != nil {
		return fmt.Errorf("failed to list models: %w", err)
	}

	// Filter to only enabled models
	var models []copilot.ModelInfo
	for _, m := range allModels {
		if m.Policy != nil && m.Policy.State == "enabled" {
			models = append(models, m)
		}
	}

	// Determine current model (from config, empty means "auto" - let Copilot choose)
	currentModel := sessionConfig.Model

	// Create event channel for TUI communication
	eventChan := make(chan tea.Msg, 100)

	// Create output channel for real-time tool output streaming
	outputChan := make(chan toolgen.OutputChunk, 100)

	// Track when forwarder goroutine exits
	var forwarderWg sync.WaitGroup

	// Forward output chunks to the TUI event channel
	forwarderWg.Go(func() {
		for chunk := range outputChan {
			eventChan <- chatui.ToolOutputChunkMsg{
				ToolID: chunk.ToolID,
				Chunk:  chunk.Chunk,
			}
		}
	})

	// Set up tools with real-time output streaming
	// Tool handlers create their own timeout context since SDK's ToolHandler interface doesn't include context.
	tools, toolMetadata := chatsvc.GetKSailToolMetadata(rootCmd, outputChan) //nolint:contextcheck

	// Create a shared agent mode reference that can be updated by the TUI
	// Default to agent mode (true = execute tools, false = plan only)
	agentModeRef := chatui.NewAgentModeRef(true)

	// Wrap tools with permission prompts and mode enforcement
	tools = WrapToolsWithPermissionAndModeMetadata(tools, eventChan, agentModeRef, toolMetadata)
	sessionConfig.Tools = tools

	// Set up permission handler for non-KSail tools (git, shell, etc.)
	sessionConfig.OnPermissionRequest = chatui.CreateTUIPermissionHandler(eventChan)

	// Create session
	session, err := client.CreateSession(sessionConfig)
	if err != nil {
		close(outputChan)
		forwarderWg.Wait()
		return fmt.Errorf("failed to create chat session: %w", err)
	}
	// Cleanup with forced exit on interrupt to prevent hanging
	defer func() {
		close(outputChan)  // Close output channel to stop forwarder goroutine
		forwarderWg.Wait() // Wait for forwarder to exit before proceeding
		select {
		case <-ctx.Done():
			// If interrupted, force exit without waiting for cleanup
			os.Exit(130) // Standard exit code for Ctrl+C
		default:
			_ = session.Destroy()
		}
	}()

	return chatui.RunWithEventChannelAndModeRef(
		ctx,
		session,
		client,
		sessionConfig,
		models,
		currentModel,
		timeout,
		eventChan,
		agentModeRef,
	)
}

// inputResult holds the result of reading from stdin.
type inputResult struct {
	input string
	err   error
}

// runChatInteractiveLoop runs the interactive chat loop.
// It handles user input and AI responses until the user exits or the context is cancelled.
//
// NOTE: The stdin reading goroutine cannot be interrupted once started, as Go's
// bufio.Reader.ReadString blocks until input or EOF. If context is cancelled
// before input arrives, one goroutine will remain blocked until process exit.
// This is a known Go limitation with blocking stdin reads.
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

		// Read input in a goroutine so we can respond to context cancellation.
		// Note: This goroutine will block on ReadString until input arrives or EOF.
		// If context is cancelled first, the goroutine remains blocked until process exit.
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
				return ctx.Err()
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
	args, ok := event.Data.Arguments.(map[string]any)
	if !ok {
		return ""
	}
	parts := make([]string, 0, len(args))
	for k, v := range args {
		parts = append(parts, fmt.Sprintf("%s=%v", k, v))
	}
	if len(parts) == 0 {
		return ""
	}
	return " (" + strings.Join(parts, ", ") + ")"
}

// formatToolArguments converts tool invocation arguments to a display string.
func formatToolArguments(args any) string {
	params, ok := args.(map[string]any)
	if !ok || len(params) == 0 {
		return ""
	}

	// Sort keys for consistent output (Go map iteration order is non-deterministic)
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	slices.Sort(keys)

	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%v", k, params[k]))
	}
	return strings.Join(parts, ", ")
}

// WrapToolsWithPermissionAndModeMetadata wraps ALL tools with mode enforcement and permission prompts.
// In plan mode, ALL tool execution is blocked (model can only describe what it would do).
// In agent mode, edit tools require permission (based on RequiresPermission annotation),
// while read-only tools are auto-approved.
func WrapToolsWithPermissionAndModeMetadata(
	tools []copilot.Tool,
	eventChan chan tea.Msg,
	agentModeRef *chatui.AgentModeRef,
	toolMetadata map[string]toolgen.ToolDefinition,
) []copilot.Tool {
	wrappedTools := make([]copilot.Tool, len(tools))

	for toolIdx, tool := range tools {
		wrappedTools[toolIdx] = tool

		// Create per-iteration copies to avoid closure capture bug.
		// Each handler must use its own tool's name and handler, not the last iteration's values.
		originalHandler := tool.Handler
		toolName := tool.Name

		wrappedTools[toolIdx].Handler = func(invocation copilot.ToolInvocation) (copilot.ToolResult, error) {
			// Check if agent mode is enabled - if not, block ALL tool execution
			if agentModeRef != nil && !agentModeRef.IsEnabled() {
				cmdDescription := strings.ReplaceAll(toolName, "_", " ")
				return copilot.ToolResult{
					TextResultForLLM: fmt.Sprintf(
						"Tool execution blocked - currently in Plan mode.\n"+
							"Tool: %s\n"+
							"In Plan mode, I can only describe what I would do, not execute tools.\n"+
							"Switch to Agent mode (press Tab) to execute tools.",
						cmdDescription,
					),
					ResultType: "failure",
					SessionLog: fmt.Sprintf("[PLAN MODE BLOCKED] %s", cmdDescription),
					Error:      fmt.Sprintf("Tool execution blocked in plan mode: %s", toolName),
				}, nil
			}

			// In agent mode, check if tool requires permission from metadata.
			// If metadata is nil or tool not found, defaults to requiresPermission=false (auto-approve).
			requiresPermission := false
			if metadata, ok := toolMetadata[toolName]; ok {
				requiresPermission = metadata.RequiresPermission
			}

			if !requiresPermission {
				// Read-only tool - execute directly
				return originalHandler(invocation)
			}

			// Edit tool - request permission
			responseChan := make(chan bool, 1)
			cmdDescription := strings.ReplaceAll(toolName, "_", " ")

			// Send permission request to TUI
			eventChan <- chatui.PermissionRequestMsg{
				ToolCallID: invocation.ToolCallID,
				ToolName:   toolName,
				Command:    cmdDescription,
				Arguments:  formatToolArguments(invocation.Arguments),
				Response:   responseChan,
			}

			// Wait for user response with timeout to prevent channel leaks
			var approved bool
			select {
			case approved = <-responseChan:
			case <-time.After(5 * time.Minute):
				return copilot.ToolResult{
					TextResultForLLM: fmt.Sprintf(
						"Permission request timed out for: %s\n"+
							"The user did not respond within the timeout period.",
						cmdDescription,
					),
					ResultType: "failure",
					SessionLog: fmt.Sprintf("[TIMEOUT] %s", cmdDescription),
				}, nil
			}

			if !approved {
				return copilot.ToolResult{
					TextResultForLLM: fmt.Sprintf(
						"Permission denied by user for: %s\n"+
							"The user chose not to allow this operation.",
						cmdDescription,
					),
					ResultType: "failure",
					SessionLog: fmt.Sprintf("[DENIED] %s", cmdDescription),
				}, nil
			}

			// User approved - execute the original handler
			return originalHandler(invocation)
		}
	}

	return wrappedTools
}

// loadChatModelFromConfig attempts to load the chat model from ksail.yaml config.
// Returns empty string if config doesn't exist or model is not set.
func loadChatModelFromConfig() string {
	// Try to load ksail.yaml from current directory
	configPath := "ksail.yaml"
	data, err := os.ReadFile(configPath)
	if err != nil {
		// Config doesn't exist or can't be read - use default
		return ""
	}

	var config v1alpha1.Cluster
	err = yaml.Unmarshal(data, &config)
	if err != nil {
		// Config exists but couldn't be parsed - ignore and use default
		return ""
	}

	return config.Spec.Chat.Model
}
