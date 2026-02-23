package chat

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	chatui "github.com/devantler-tech/ksail/v5/pkg/cli/ui/chat"
	chatsvc "github.com/devantler-tech/ksail/v5/pkg/svc/chat"
	"github.com/devantler-tech/ksail/v5/pkg/toolgen"
	copilot "github.com/github/copilot-sdk/go"
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
) (*chatui.ChatModeRef, *chatui.YoloModeRef, error) {
	tools, toolMetadata := chatsvc.GetKSailToolMetadata(rootCmd, outputChan)
	chatModeRef := chatui.NewChatModeRef(chatui.AgentMode)
	yoloModeRef := chatui.NewYoloModeRef(false)
	tools = WrapToolsWithPermissionAndModeMetadata(
		tools, eventChan, chatModeRef, yoloModeRef, toolMetadata,
	)
	sessionConfig.Tools = tools
	sessionConfig.OnPermissionRequest = chatui.CreateTUIPermissionHandler(eventChan, yoloModeRef)

	allowedRoot, err := os.Getwd()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to determine working directory for sandboxing: %w", err)
	}

	sessionConfig.Hooks = &copilot.SessionHooks{
		OnPreToolUse: BuildPreToolUseHook(chatModeRef, toolMetadata, allowedRoot),
	}

	return chatModeRef, yoloModeRef, nil
}

// runTUIChat starts the TUI chat mode.
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

	chatModeRef, yoloModeRef, err := setupChatTools( //nolint:contextcheck
		sessionConfig, rootCmd, eventChan, outputChan,
	)
	if err != nil {
		close(outputChan)
		forwarderWg.Wait()

		return err
	}

	session, err := client.CreateSession(ctx, sessionConfig)
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

	err = chatui.Run(ctx, chatui.Params{
		Session:       session,
		Client:        client,
		SessionConfig: sessionConfig,
		Models:        nil, // Lazy-loaded on first ^O press
		CurrentModel:  currentModel,
		Timeout:       timeout,
		EventChan:     eventChan,
		ChatModeRef:   chatModeRef,
		YoloModeRef:   yoloModeRef,
		Theme:         chatui.DefaultThemeConfig(),
		ToolDisplay:   chatui.DefaultToolDisplayConfig(),
	})
	if err != nil {
		return fmt.Errorf("TUI chat failed: %w", err)
	}

	return nil
}
