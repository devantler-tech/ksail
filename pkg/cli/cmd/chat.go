package cmd

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

	tea "github.com/charmbracelet/bubbletea"
	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/cli/annotations"
	chatui "github.com/devantler-tech/ksail/v5/pkg/cli/ui/chat"
	"github.com/devantler-tech/ksail/v5/pkg/di"
	"github.com/devantler-tech/ksail/v5/pkg/notify"
	chatsvc "github.com/devantler-tech/ksail/v5/pkg/svc/chat"
	"github.com/devantler-tech/ksail/v5/pkg/toolgen"
	copilot "github.com/github/copilot-sdk/go"
	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"
)

// Chat command constants.
const (
	// signalExitCode is the standard exit code for Ctrl+C / SIGINT.
	signalExitCode = 130
	// eventChannelBuffer is the buffer size for TUI event channels.
	eventChannelBuffer = 100
	// outputChannelBuffer is the buffer size for tool output streaming channels.
	outputChannelBuffer = 100
	// defaultTimeoutMinutes is the default response timeout in minutes.
	defaultTimeoutMinutes = 5
	// signalSleepDuration is the delay before exiting after a signal to allow cleanup.
	signalSleepDuration = 50 * time.Millisecond
	// permissionTimeoutMinutes is the timeout for permission requests.
	permissionTimeoutMinutes = 5
)

// Sentinel errors for the chat command.
var (
	errNotAuthenticated = errors.New("not authenticated with GitHub Copilot")
	errResponseTimeout  = errors.New("response timeout")
	errSessionError     = errors.New("session error")
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
			"%w\n\n"+
				"To fix:\n"+
				"  1. Run: gh auth login\n"+
				"  2. Ensure you have an active GitHub Copilot subscription",
			errNotAuthenticated,
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
func NewChatCmd(_ *di.Runtime) *cobra.Command {
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
	cmd.Flags().DurationP(
		"timeout", "t", defaultTimeoutMinutes*time.Minute,
		"Response timeout duration",
	)
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
		time.Sleep(signalSleepDuration)
		os.Exit(signalExitCode)
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
			os.Exit(signalExitCode)
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
			Content: "Authenticated as " + loginName,
			Writer:  writer,
		})
	}

	systemContext, err := chatsvc.BuildSystemContext()
	if err != nil && !flags.useTUI {
		notify.WriteMessage(notify.Message{
			Type:    notify.WarningType,
			Content: "Could not load full context: " + err.Error(),
			Writer:  writer,
		})
	}

	sessionConfig := buildSessionConfig(flags.model, flags.streaming, systemContext)

	if flags.useTUI {
		return runTUIChat(ctx, client, sessionConfig, flags.timeout, cmd.Root())
	}

	return runNonTUIChat(ctx, client, sessionConfig, flags, cmd, writer)
}

// filterEnabledModels returns only models with an enabled policy state.
func filterEnabledModels(allModels []copilot.ModelInfo) []copilot.ModelInfo {
	var models []copilot.ModelInfo

	for _, m := range allModels {
		if m.Policy != nil && m.Policy.State == "enabled" {
			models = append(models, m)
		}
	}

	return models
}

// startOutputForwarder forwards tool output chunks to the TUI event channel.
// Returns a WaitGroup that completes when the forwarder goroutine exits.
func startOutputForwarder(
	outputChan <-chan toolgen.OutputChunk,
	eventChan chan<- tea.Msg,
) *sync.WaitGroup {
	var forwarderWg sync.WaitGroup

	forwarderWg.Go(func() {
		for chunk := range outputChan {
			eventChan <- chatui.ToolOutputChunkMsg{
				ToolID: chunk.ToolID,
				Chunk:  chunk.Chunk,
			}
		}
	})

	return &forwarderWg
}

// runTUIChat starts the TUI chat mode.
func runTUIChat(
	ctx context.Context,
	client *copilot.Client,
	sessionConfig *copilot.SessionConfig,
	timeout time.Duration,
	rootCmd *cobra.Command,
) error {
	allModels, err := client.ListModels()
	if err != nil {
		return fmt.Errorf("failed to list models: %w", err)
	}

	models := filterEnabledModels(allModels)
	currentModel := sessionConfig.Model
	eventChan := make(chan tea.Msg, eventChannelBuffer)
	outputChan := make(chan toolgen.OutputChunk, outputChannelBuffer)
	forwarderWg := startOutputForwarder(outputChan, eventChan)

	// Tool handlers create their own timeout context since SDK's ToolHandler interface doesn't include context.
	tools, toolMetadata := chatsvc.GetKSailToolMetadata(rootCmd, outputChan) //nolint:contextcheck
	agentModeRef := chatui.NewAgentModeRef(true)
	tools = WrapToolsWithPermissionAndModeMetadata(tools, eventChan, agentModeRef, toolMetadata)
	sessionConfig.Tools = tools
	sessionConfig.OnPermissionRequest = chatui.CreateTUIPermissionHandler(eventChan)

	session, err := client.CreateSession(sessionConfig)
	if err != nil {
		close(outputChan)
		forwarderWg.Wait()

		return fmt.Errorf("failed to create chat session: %w", err)
	}

	defer func() {
		close(outputChan)
		forwarderWg.Wait()

		select {
		case <-ctx.Done():
			os.Exit(signalExitCode)
		default:
			_ = session.Destroy()
		}
	}()

	return fmt.Errorf(
		"TUI chat failed: %w",
		chatui.RunWithEventChannelAndModeRef(
			ctx, session, client, sessionConfig,
			models, currentModel, timeout, eventChan, agentModeRef,
		),
	)
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

// buildPlanModeBlockedResult creates a ToolResult indicating tool execution was blocked in plan mode.
func buildPlanModeBlockedResult(toolName string) (copilot.ToolResult, error) {
	cmdDescription := strings.ReplaceAll(toolName, "_", " ")

	return copilot.ToolResult{
		TextResultForLLM: "Tool execution blocked - currently in Plan mode.\n" +
			"Tool: " + cmdDescription + "\n" +
			"In Plan mode, I can only describe what I would do, not execute tools.\n" +
			"Switch to Agent mode (press Tab) to execute tools.",
		ResultType: "failure",
		SessionLog: "[PLAN MODE BLOCKED] " + cmdDescription,
		Error:      "Tool execution blocked in plan mode: " + toolName,
	}, nil
}

// awaitToolPermission sends a permission request to the TUI and waits for user response.
// Returns the approval state and an optional denial/timeout ToolResult.
func awaitToolPermission(
	eventChan chan tea.Msg,
	toolName string,
	invocation copilot.ToolInvocation,
) (bool, *copilot.ToolResult) {
	responseChan := make(chan bool, 1)
	cmdDescription := strings.ReplaceAll(toolName, "_", " ")

	eventChan <- chatui.PermissionRequestMsg{
		ToolCallID: invocation.ToolCallID,
		ToolName:   toolName,
		Command:    cmdDescription,
		Arguments:  formatToolArguments(invocation.Arguments),
		Response:   responseChan,
	}

	var approved bool

	select {
	case approved = <-responseChan:
	case <-time.After(permissionTimeoutMinutes * time.Minute):
		return false, &copilot.ToolResult{
			TextResultForLLM: "Permission request timed out for: " + cmdDescription + "\n" +
				"The user did not respond within the timeout period.",
			ResultType: "failure",
			SessionLog: "[TIMEOUT] " + cmdDescription,
		}
	}

	if !approved {
		return false, &copilot.ToolResult{
			TextResultForLLM: "Permission denied by user for: " + cmdDescription + "\n" +
				"The user chose not to allow this operation.",
			ResultType: "failure",
			SessionLog: "[DENIED] " + cmdDescription,
		}
	}

	return true, nil
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
			if agentModeRef != nil && !agentModeRef.IsEnabled() {
				return buildPlanModeBlockedResult(toolName)
			}

			// Check if tool requires permission from metadata.
			// If metadata is nil or tool not found, defaults to requiresPermission=false (auto-approve).
			requiresPermission := false
			if metadata, ok := toolMetadata[toolName]; ok {
				requiresPermission = metadata.RequiresPermission
			}

			if !requiresPermission {
				return originalHandler(invocation)
			}

			approved, result := awaitToolPermission(eventChan, toolName, invocation)
			if !approved {
				return *result, nil
			}

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
