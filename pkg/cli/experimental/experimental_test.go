package experimental_test

import (
	"io"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/cli/experimental"
	"github.com/devantler-tech/ksail/v7/pkg/cli/flags"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// guardedCmd builds a minimal command carrying the --experimental opt-in flag and
// gated by experimental.Guard. ran records whether the inner RunE executed.
func guardedCmd(ran *bool) *cobra.Command {
	cmd := &cobra.Command{
		Use:          "demo",
		SilenceUsage: true,
		RunE: func(_ *cobra.Command, _ []string) error {
			*ran = true

			return nil
		},
	}
	cmd.Flags().Bool(flags.ExperimentalFlagName, false, "")

	return experimental.Guard(cmd)
}

func TestGuardHidesCommand(t *testing.T) {
	t.Parallel()

	var ran bool

	cmd := guardedCmd(&ran)

	assert.True(t, cmd.Hidden, "a guarded command must be hidden from help and tool surfaces")
}

func TestGuardNilReturnsNil(t *testing.T) {
	t.Parallel()

	assert.Nil(t, experimental.Guard(nil))
}

// TestGuardRefusesWithoutOptIn covers the OFF state: no --experimental → refuse.
func TestGuardRefusesWithoutOptIn(t *testing.T) {
	t.Parallel()

	var ran bool

	cmd := guardedCmd(&ran)
	cmd.SetArgs([]string{})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	err := cmd.Execute()

	require.ErrorIs(t, err, experimental.ErrDisabled)
	assert.False(t, ran, "the inner RunE must not run when experimental is disabled")
}

// TestGuardRunsWithOptIn covers the ON state: --experimental → run.
func TestGuardRunsWithOptIn(t *testing.T) {
	t.Parallel()

	var ran bool

	cmd := guardedCmd(&ran)
	cmd.SetArgs([]string{"--experimental"})
	cmd.SetOut(io.Discard)
	cmd.SetErr(io.Discard)

	require.NoError(t, cmd.Execute())
	assert.True(t, ran, "the inner RunE must run when experimental is enabled")
}

// TestGuardNilInnerRunE ensures a gated command with no RunE (e.g. a parent) is a
// no-op when opted in, rather than panicking on a nil inner.
func TestGuardNilInnerRunE(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{Use: "demo", SilenceUsage: true}
	cmd.Flags().Bool(flags.ExperimentalFlagName, true, "")

	guarded := experimental.Guard(cmd)
	guarded.SetArgs([]string{})
	guarded.SetOut(io.Discard)
	guarded.SetErr(io.Discard)

	require.NoError(t, guarded.Execute())
}
