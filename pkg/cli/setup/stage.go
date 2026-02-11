package setup

import (
	"context"
	"fmt"

	dockerhelpers "github.com/devantler-tech/ksail/v5/pkg/cli/dockerutil"
	"github.com/devantler-tech/ksail/v5/pkg/cli/flags"
	"github.com/devantler-tech/ksail/v5/pkg/notify"
	"github.com/devantler-tech/ksail/v5/pkg/timer"
	"github.com/docker/docker/client"
	"github.com/spf13/cobra"
)

// StageInfo contains display information for a setup stage.
// Note: Leading newlines between stages are handled automatically by StageSeparatingWriter.
type StageInfo struct {
	Title         string
	Emoji         string
	Activity      string
	Success       string
	FailurePrefix string
}

// DockerClientInvoker is a function that invokes an operation with a Docker client.
type DockerClientInvoker func(*cobra.Command, func(client.APIClient) error) error

// DefaultDockerClientInvoker returns the default Docker client invoker.
func DefaultDockerClientInvoker() DockerClientInvoker {
	return dockerhelpers.WithDockerClient
}

// RunDockerStage executes a Docker-based stage with standard progress messaging.
// This provides a consistent pattern for all Docker operations that need to:
// 1. Track timing via Timer.NewStage()
// 2. Show a title message with emoji
// 3. Show an activity message (optional)
// 4. Execute a Docker action
// 5. Show success/failure messages
//
// Parameters:
//   - cmd: The Cobra command for output context
//   - tmr: Timer for stage timing
//   - info: Stage display information
//   - action: The Docker action to execute
//   - dockerInvoker: Function to create/invoke Docker client (use nil for default)
func RunDockerStage(
	cmd *cobra.Command,
	tmr timer.Timer,
	info StageInfo,
	action func(context.Context, client.APIClient) error,
	dockerInvoker DockerClientInvoker,
) error {
	tmr.NewStage()

	notify.WriteMessage(notify.Message{
		Type:    notify.TitleType,
		Content: info.Title,
		Emoji:   info.Emoji,
		Writer:  cmd.OutOrStdout(),
	})

	if info.Activity != "" {
		notify.WriteMessage(notify.Message{
			Type:    notify.ActivityType,
			Content: info.Activity,
			Writer:  cmd.OutOrStdout(),
		})
	}

	invoker := dockerInvoker
	if invoker == nil {
		invoker = dockerhelpers.WithDockerClient
	}

	err := invoker(cmd, func(dockerClient client.APIClient) error {
		actionErr := action(cmd.Context(), dockerClient)
		if actionErr != nil {
			return fmt.Errorf("%s: %w", info.FailurePrefix, actionErr)
		}

		outputTimer := flags.MaybeTimer(cmd, tmr)

		notify.WriteMessage(notify.Message{
			Type:    notify.SuccessType,
			Content: info.Success,
			Timer:   outputTimer,
			Writer:  cmd.OutOrStdout(),
		})

		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to execute stage: %w", err)
	}

	return nil
}
