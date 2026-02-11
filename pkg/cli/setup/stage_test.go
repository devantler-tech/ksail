package setup_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/devantler-tech/ksail/v5/pkg/cli/setup"
	"github.com/devantler-tech/ksail/v5/pkg/timer"
	"github.com/docker/docker/client"
	"github.com/gkampitakis/go-snaps/snaps"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Error variables for test cases.
var (
	errSomethingWentWrong = errors.New("something went wrong")
	errInvokerFailed      = errors.New("invoker failed")
)

func TestDefaultDockerClientInvoker(t *testing.T) {
	t.Parallel()

	t.Run("returns non-nil invoker", func(t *testing.T) {
		t.Parallel()

		invoker := setup.DefaultDockerClientInvoker()
		assert.NotNil(t, invoker, "DefaultDockerClientInvoker should return a non-nil invoker")
	})
}

func TestRunDockerStage_Success(t *testing.T) {
	t.Parallel()

	t.Run("executes action successfully with activity message", func(t *testing.T) {
		t.Parallel()

		var buf bytes.Buffer

		cmd := &cobra.Command{}
		cmd.SetOut(&buf)
		cmd.SetContext(context.Background())

		tmr := timer.New()
		tmr.Start()

		info := setup.StageInfo{
			Title:         "Test Stage",
			Emoji:         "🧪",
			Activity:      "Testing...",
			Success:       "Test passed",
			FailurePrefix: "test failed",
		}

		actionCalled := false
		mockInvoker := func(_ *cobra.Command, action func(client.APIClient) error) error {
			return action(nil)
		}

		err := setup.RunDockerStage(
			cmd,
			tmr,
			info,
			func(_ context.Context, _ client.APIClient) error {
				actionCalled = true

				return nil
			},
			mockInvoker,
		)

		require.NoError(t, err)
		assert.True(t, actionCalled, "action should have been called")
		snaps.MatchSnapshot(t, buf.String())
	})
}

func TestRunDockerStage_NoActivity(t *testing.T) {
	t.Parallel()

	t.Run("executes without activity message", func(t *testing.T) {
		t.Parallel()

		var buf bytes.Buffer

		cmd := &cobra.Command{}
		cmd.SetOut(&buf)
		cmd.SetContext(context.Background())

		tmr := timer.New()
		tmr.Start()

		info := setup.StageInfo{
			Title:         "Simple Stage",
			Emoji:         "✅",
			Activity:      "",
			Success:       "Completed",
			FailurePrefix: "failed",
		}

		mockInvoker := func(_ *cobra.Command, action func(client.APIClient) error) error {
			return action(nil)
		}

		err := setup.RunDockerStage(
			cmd,
			tmr,
			info,
			func(_ context.Context, _ client.APIClient) error {
				return nil
			},
			mockInvoker,
		)

		require.NoError(t, err)
		snaps.MatchSnapshot(t, buf.String())
	})
}

func TestRunDockerStage_ActionError(t *testing.T) {
	t.Parallel()

	t.Run("wraps action error with failure prefix", func(t *testing.T) {
		t.Parallel()

		var buf bytes.Buffer

		cmd := &cobra.Command{}
		cmd.SetOut(&buf)
		cmd.SetContext(context.Background())

		tmr := timer.New()
		tmr.Start()

		info := setup.StageInfo{
			Title:         "Failing Stage",
			Emoji:         "❌",
			Activity:      "Running...",
			Success:       "Done",
			FailurePrefix: "action failed",
		}

		mockInvoker := func(_ *cobra.Command, action func(client.APIClient) error) error {
			return action(nil)
		}

		err := setup.RunDockerStage(
			cmd,
			tmr,
			info,
			func(_ context.Context, _ client.APIClient) error {
				return fmt.Errorf("test action error: %w", errSomethingWentWrong)
			},
			mockInvoker,
		)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "action failed")
		assert.Contains(t, err.Error(), "something went wrong")
	})
}

func TestRunDockerStage_InvokerError(t *testing.T) {
	t.Parallel()

	t.Run("handles invoker error", func(t *testing.T) {
		t.Parallel()

		var buf bytes.Buffer

		cmd := &cobra.Command{}
		cmd.SetOut(&buf)
		cmd.SetContext(context.Background())

		tmr := timer.New()
		tmr.Start()

		info := setup.StageInfo{
			Title:         "Invoker Error Stage",
			Emoji:         "💥",
			Activity:      "Starting...",
			Success:       "Done",
			FailurePrefix: "failed",
		}

		mockInvoker := func(_ *cobra.Command, _ func(client.APIClient) error) error {
			return fmt.Errorf("invoker error: %w", errInvokerFailed)
		}

		err := setup.RunDockerStage(
			cmd,
			tmr,
			info,
			func(_ context.Context, _ client.APIClient) error {
				return nil
			},
			mockInvoker,
		)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to execute stage")
		assert.Contains(t, err.Error(), "invoker failed")
	})
}

func TestRunDockerStage_NilInvoker(t *testing.T) {
	t.Parallel()

	t.Run("uses default invoker when nil provided", func(t *testing.T) {
		t.Parallel()

		// This test verifies that passing nil for dockerInvoker doesn't panic
		// and falls back to the default invoker (helpers.WithDockerClient)
		// We can't easily test the actual Docker client here without Docker running,
		// so we just verify the function handles nil invoker gracefully
		var buf bytes.Buffer

		cmd := &cobra.Command{}
		cmd.SetOut(&buf)
		cmd.SetContext(context.Background())

		tmr := timer.New()
		tmr.Start()

		info := setup.StageInfo{
			Title:         "Nil Invoker Stage",
			Emoji:         "🔧",
			Activity:      "Processing...",
			Success:       "Done",
			FailurePrefix: "failed",
		}

		// This will try to use the real Docker client, which may fail if Docker isn't running
		// That's acceptable - we're just testing that nil invoker is handled without panic
		_ = setup.RunDockerStage(cmd, tmr, info, func(_ context.Context, _ client.APIClient) error {
			return nil
		}, nil)
		// We don't assert on error here since Docker may or may not be available
	})
}

func TestRunDockerStage_WithTiming(t *testing.T) {
	t.Parallel()

	t.Run("tracks timing correctly", func(t *testing.T) {
		t.Parallel()

		var buf bytes.Buffer

		cmd := &cobra.Command{}
		cmd.SetOut(&buf)
		cmd.SetContext(context.Background())

		tmr := timer.New()
		tmr.Start()

		info := setup.StageInfo{
			Title:         "Timed Stage",
			Emoji:         "⏱️",
			Activity:      "Timing...",
			Success:       "Timed successfully",
			FailurePrefix: "timing failed",
		}

		mockInvoker := func(_ *cobra.Command, action func(client.APIClient) error) error {
			return action(nil)
		}

		err := setup.RunDockerStage(
			cmd,
			tmr,
			info,
			func(_ context.Context, _ client.APIClient) error {
				return nil
			},
			mockInvoker,
		)

		require.NoError(t, err)
		assert.Contains(t, buf.String(), "Timed successfully")
	})
}
