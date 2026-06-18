package chat

import (
	"context"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/cli/annotations"
	"github.com/devantler-tech/ksail/v7/pkg/notify"
	chatsvc "github.com/devantler-tech/ksail/v7/pkg/svc/chat"
	copilot "github.com/github/copilot-sdk/go"
	"github.com/spf13/cobra"
)

// Chat command constants shared across the package.
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
)

// buildSessionConfig creates the Copilot session configuration.
func buildSessionConfig(
	model string,
	reasoningEffort string,
	streaming bool,
	sections map[string]copilot.SectionOverride,
) *copilot.SessionConfig {
	backgroundThreshold := 0.80
	exhaustionThreshold := 0.95

	config := &copilot.SessionConfig{
		Streaming: &streaming,
		SystemMessage: &copilot.SystemMessageConfig{
			Mode:     "customize",
			Sections: sections,
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
func NewChatCmd() *cobra.Command {
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
  - An active GitHub Copilot subscription

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

// handleChatRunE handles the chat command execution.
func handleChatRunE(cmd *cobra.Command) error {
	writer := cmd.OutOrStdout()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	flags, err := parseChatFlags(cmd)
	if err != nil {
		return err
	}

	if !flags.useTUI {
		setupNonTUISignalHandler(cancel, writer)
		notifyNonTUIStartup(writer)
	}

	client, loginName, cleanup, err := setupCopilotClient(ctx)
	if err != nil {
		return err
	}

	defer cleanup()

	if !flags.useTUI {
		notify.WriteMessage(notify.Message{
			Type:    notify.InfoType,
			Content: "Authenticated as " + loginName,
			Writer:  writer,
		})
	}

	sections := chatsvc.BuildSystemSections(cmd.Root())

	sessionConfig := buildSessionConfig(
		flags.model,
		flags.reasoningEffort,
		flags.streaming,
		sections,
	)

	if flags.useTUI {
		return runTUIChat(ctx, client, sessionConfig, flags.timeout, cmd.Root())
	}

	return runNonTUIChat(ctx, client, sessionConfig, flags, cmd, writer)
}
