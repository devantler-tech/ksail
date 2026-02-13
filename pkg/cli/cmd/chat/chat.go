package chat

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/devantler-tech/ksail/v5/pkg/apis/cluster/v1alpha1"
	"github.com/devantler-tech/ksail/v5/pkg/cli/annotations"
	"github.com/devantler-tech/ksail/v5/pkg/di"
	"github.com/devantler-tech/ksail/v5/pkg/notify"
	chatsvc "github.com/devantler-tech/ksail/v5/pkg/svc/chat"
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

// flags holds parsed flags for the chat command.
type flags struct {
	model           string
	reasoningEffort string
	streaming       bool
	timeout         time.Duration
	useTUI          bool
}

// parseChatFlags extracts and resolves chat command flags.
func parseChatFlags(cmd *cobra.Command) flags {
	modelFlag, _ := cmd.Flags().GetString("model")
	reasoningEffort, _ := cmd.Flags().GetString("reasoning-effort")
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

	return flags{
		model:           model,
		reasoningEffort: reasoningEffort,
		streaming:       streaming,
		timeout:         timeout,
		useTUI:          useTUI,
	}
}

// startCopilotClient creates and starts a Copilot client.
// If GITHUB_TOKEN is set, it is used for authentication, bypassing OAuth/gh CLI auth.
func startCopilotClient(ctx context.Context) (*copilot.Client, error) {
	opts := &copilot.ClientOptions{
		LogLevel: "error",
	}

	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		opts.GithubToken = token
	}

	client := copilot.NewClient(opts)

	err := client.Start(ctx)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to start Copilot client: %w\n\n"+
				"To fix:\n"+
				"  1. Install GitHub Copilot CLI: npm install -g @githubnext/github-copilot-cli\n"+
				"  2. Or set COPILOT_CLI_PATH to your installation\n"+
				"  3. Or set GITHUB_TOKEN for token-based authentication",
			err,
		)
	}

	return client, nil
}

// validateCopilotAuth checks authentication and returns the login name.
func validateCopilotAuth(ctx context.Context, client *copilot.Client) (string, error) {
	authStatus, err := client.GetAuthStatus(ctx)
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
	reasoningEffort string,
	streaming bool,
	systemContext string,
) *copilot.SessionConfig {
	backgroundThreshold := 0.80
	exhaustionThreshold := 0.95

	config := &copilot.SessionConfig{
		Streaming: streaming,
		SystemMessage: &copilot.SystemMessageConfig{
			Mode:    "append",
			Content: systemContext,
		},
		InfiniteSessions: &copilot.InfiniteSessionConfig{
			Enabled:                       new(true),
			BackgroundCompactionThreshold: &backgroundThreshold,
			BufferExhaustionThreshold:     &exhaustionThreshold,
		},
	}

	if model != "" {
		config.Model = model
	}

	if reasoningEffort != "" {
		config.ReasoningEffort = reasoningEffort
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
	cmd.Flags().StringP(
		"reasoning-effort", "r", "",
		"Reasoning effort level for models that support it (low, medium, high)",
	)
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

// notifyNonTUIStartup sends startup notifications when running outside the TUI.
func notifyNonTUIStartup(writer io.Writer) {
	notify.WriteMessage(notify.Message{
		Type:    notify.TitleType,
		Content: "Starting KSail AI Assistant...",
		Emoji:   "🤖",
		Writer:  writer,
	})
}

// handleChatRunE handles the chat command execution.
func handleChatRunE(cmd *cobra.Command) error {
	writer := cmd.OutOrStdout()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	flags := parseChatFlags(cmd)

	if !flags.useTUI {
		setupNonTUISignalHandler(cancel, writer)
		notifyNonTUIStartup(writer)
	}

	client, err := startCopilotClient(ctx)
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

	loginName, err := validateCopilotAuth(ctx, client)
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

	sessionConfig := buildSessionConfig(
		flags.model,
		flags.reasoningEffort,
		flags.streaming,
		systemContext,
	)

	if flags.useTUI {
		return runTUIChat(ctx, client, sessionConfig, flags.timeout, cmd.Root())
	}

	return runNonTUIChat(ctx, client, sessionConfig, flags, cmd, writer)
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
