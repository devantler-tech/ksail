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
	allModels, err := client.ListModels(context.Background())
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
	yoloModeRef := chatui.NewYoloModeRef(false)
	tools = WrapToolsWithPermissionAndModeMetadata(
		tools, eventChan, agentModeRef, yoloModeRef, toolMetadata,
	)
	sessionConfig.Tools = tools
	sessionConfig.OnPermissionRequest = chatui.CreateTUIPermissionHandler(eventChan, yoloModeRef)

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

	err = chatui.RunWithEventChannelAndModeRef(
		ctx, session, client, sessionConfig,
		models, currentModel, timeout, eventChan, agentModeRef, yoloModeRef,
	)
	if err != nil {
		return fmt.Errorf("TUI chat failed: %w", err)
	}

	return nil
}
