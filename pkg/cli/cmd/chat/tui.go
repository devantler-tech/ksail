package chat

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	chatui "github.com/devantler-tech/ksail/v7/pkg/cli/ui/chat"
	chatsvc "github.com/devantler-tech/ksail/v7/pkg/svc/chat"
	"github.com/devantler-tech/ksail/v7/pkg/toolgen"
	copilot "github.com/github/copilot-sdk/go"
	"github.com/github/copilot-sdk/go/rpc"
	"github.com/spf13/cobra"
)

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

// setupChatTools configures the chat tools, permission and mode references.
func setupChatTools(
	sessionConfig *copilot.SessionConfig,
	rootCmd *cobra.Command,
	eventChan chan tea.Msg,
	outputChan chan toolgen.OutputChunk,
	sessionLog *toolgen.SessionLogRef,
) (*chatui.ChatModeRef, error) {
	tools, toolMetadata := chatsvc.GetKSailToolMetadata(rootCmd, outputChan, sessionLog)
	chatModeRef := chatui.NewChatModeRef(chatui.InteractiveMode)
	tools = WrapToolsWithForceInjection(tools, toolMetadata)
	sessionConfig.Tools = tools
	sessionConfig.OnPermissionRequest = chatui.CreateTUIPermissionHandler(eventChan, chatModeRef)

	allowedRoot, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("failed to determine working directory for sandboxing: %w", err)
	}

	sessionConfig.Hooks = &copilot.SessionHooks{
		OnPreToolUse: BuildPreToolUseHook(allowedRoot),
	}

	return chatModeRef, nil
}

// buildTUIOnEventHandler creates an OnEvent handler for the TUI that dispatches
// session-level events to the event channel. It handles events NOT covered by the
// per-turn session.On() dispatcher, ensuring events during session creation and
// between turns are captured.
func buildTUIOnEventHandler(eventChan chan<- tea.Msg) copilot.SessionEventHandler {
	return func(event copilot.SessionEvent) {
		//nolint:exhaustive // Only session-level events not in per-turn dispatcher; rest ignored.
		switch event.Type() {
		case copilot.SessionEventTypeToolExecutionProgress:
			if data, ok := event.Data.(*copilot.ToolExecutionProgressData); ok {
				eventChan <- chatui.ToolProgressMsg{
					ToolID:  data.ToolCallID,
					Message: data.ProgressMessage,
				}
			}
		case copilot.SessionEventTypeSessionTaskComplete:
			msg := ""

			data, isTaskComplete := event.Data.(*copilot.SessionTaskCompleteData)
			if isTaskComplete && data.Summary != nil {
				msg = *data.Summary
			}

			eventChan <- chatui.TaskCompleteMsg{Message: msg}
		}
	}
}

// wireSessionLog wires the session's RPC.Log method into the SessionLogRef
// so tool handlers can log to the session during execution.
func wireSessionLog(session *copilot.Session, logRef *toolgen.SessionLogRef) {
	if logRef == nil {
		return
	}

	logRef.Set(func(ctx context.Context, message, level string) {
		l := rpc.SessionLogLevel(level)
		_, _ = session.RPC.Log(ctx, &rpc.LogRequest{
			Message: message,
			Level:   &l,
		})
	})
}

// runTUIChat starts the TUI chat mode.
//
//nolint:funlen // session lifecycle setup requires sequential steps
func runTUIChat(
	ctx context.Context,
	client *copilot.Client,
	sessionConfig *copilot.SessionConfig,
	timeout time.Duration,
	rootCmd *cobra.Command,
) error {
	currentModel := sessionConfig.Model
	eventChan := make(chan tea.Msg, eventChannelBuffer)
	outputChan := make(chan toolgen.OutputChunk, outputChannelBuffer)
	forwarderWg := startOutputForwarder(outputChan, eventChan)

	// Create session log ref for SDK-native tool logging
	sessionLog := toolgen.NewSessionLogRef()

	// Register OnEvent handler to catch session-level events during creation and between turns
	sessionConfig.OnEvent = buildTUIOnEventHandler(eventChan)

	chatModeRef, err := setupChatTools(
		sessionConfig, rootCmd, eventChan, outputChan, sessionLog,
	)
	if err != nil {
		close(outputChan)
		forwarderWg.Wait()

		return err
	}

	// Register slash commands for the TUI
	sessionConfig.Commands = chatui.BuildTUISlashCommands(eventChan)

	// Register elicitation handler for MCP tool form requests
	sessionConfig.OnElicitationRequest = chatui.CreateTUIElicitationHandler(eventChan)

	session, err := client.CreateSession(ctx, sessionConfig)
	if err != nil {
		close(outputChan)
		forwarderWg.Wait()

		return fmt.Errorf("failed to create chat session: %w", err)
	}

	// Wire session log now that the session exists
	wireSessionLog(session, sessionLog)

	defer func() {
		close(outputChan)
		forwarderWg.Wait()

		select {
		case <-ctx.Done():
			os.Exit(signalExitCode)
		default:
			_ = session.Disconnect()
		}
	}()

	err = chatui.Run(ctx, chatui.Params{
		Session:       session,
		Client:        client,
		SessionConfig: sessionConfig,
		Models:        nil, // Lazy-loaded on first ^O press
		CurrentModel:  currentModel,
		Timeout:       timeout,
		EventChan:     eventChan,
		ChatModeRef:   chatModeRef,
		Theme:         chatui.DefaultThemeConfig(),
		ToolDisplay:   chatui.DefaultToolDisplayConfig(),
	})
	if err != nil {
		return fmt.Errorf("TUI chat failed: %w", err)
	}

	return nil
}
