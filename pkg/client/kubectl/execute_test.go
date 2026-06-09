package kubectl_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/client/kubectl"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
	cmdutil "k8s.io/kubectl/pkg/cmd/util"
)

// errBoom is a sentinel error used to exercise the failure paths of
// ExecuteSafely without coupling to any specific message text.
var errBoom = errors.New("boom")

// newSilentCmd builds a cobra command wired for silent, in-process execution
// (no usage/error printing, no os.Args parsing) so ExecuteSafely can be tested
// without touching a cluster or the real process state.
func newSilentCmd(run func(cmd *cobra.Command, args []string)) *cobra.Command {
	cmd := &cobra.Command{
		Use:           "test",
		SilenceUsage:  true,
		SilenceErrors: true,
		Run:           run,
	}
	cmd.SetArgs([]string{})
	cmd.SetOut(&strings.Builder{})
	cmd.SetErr(&strings.Builder{})

	return cmd
}

// TestExecuteSafelySuccess covers the happy path: a command that runs cleanly
// returns no error.
func TestExecuteSafelySuccess(t *testing.T) {
	t.Parallel()

	cmd := newSilentCmd(func(_ *cobra.Command, _ []string) {})

	err := kubectl.ExecuteSafely(context.Background(), cmd)
	require.NoError(t, err)
}

// TestExecuteSafelyCommandError covers the path where the cobra command returns
// a normal (non-fatal) error: ExecuteSafely wraps it with the "kubectl command
// failed" prefix while preserving the underlying error for errors.Is.
func TestExecuteSafelyCommandError(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{
		Use:           "test",
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(_ *cobra.Command, _ []string) error {
			return errBoom
		},
	}
	cmd.SetArgs([]string{})
	cmd.SetOut(&strings.Builder{})
	cmd.SetErr(&strings.Builder{})

	err := kubectl.ExecuteSafely(context.Background(), cmd)
	require.Error(t, err)
	require.ErrorIs(t, err, errBoom)
	require.Contains(t, err.Error(), "kubectl command failed")
}

// TestExecuteSafelyFatalError covers the core safety mechanism: a command that
// triggers kubectl's CheckErr/BehaviorOnFatal (which would normally os.Exit) is
// recovered and returned as an error instead of terminating the process.
func TestExecuteSafelyFatalError(t *testing.T) {
	t.Parallel()

	cmd := newSilentCmd(func(_ *cobra.Command, _ []string) {
		// CheckErr routes through BehaviorOnFatal, which ExecuteSafely overrides
		// to panic-and-recover rather than os.Exit.
		cmdutil.CheckErr(errBoom)
	})

	err := kubectl.ExecuteSafely(context.Background(), cmd)
	require.Error(t, err)
	require.Contains(t, err.Error(), "boom")
}

// TestExecuteSafelyUnexpectedPanicPropagates verifies that a panic which is NOT
// a kubectl fatal error is re-raised (not swallowed as a returned error), and
// that the fatal mutex is released so a subsequent call does not deadlock.
func TestExecuteSafelyUnexpectedPanicPropagates(t *testing.T) {
	t.Parallel()

	cmd := newSilentCmd(func(_ *cobra.Command, _ []string) {
		panic("unexpected")
	})

	require.PanicsWithValue(t, "unexpected", func() {
		_ = kubectl.ExecuteSafely(context.Background(), cmd)
	})

	// The mutex must have been unlocked before re-panicking; a follow-up call
	// proves there is no deadlock and global state was restored cleanly.
	ok := newSilentCmd(func(_ *cobra.Command, _ []string) {})
	require.NoError(t, kubectl.ExecuteSafely(context.Background(), ok))
}

// TestExecuteSafelyConcurrent exercises the package-global BehaviorOnFatal
// override under concurrency: many goroutines each trigger the fatal path, and
// all must return an error without racing or deadlocking. Run with -race.
func TestExecuteSafelyConcurrent(t *testing.T) {
	t.Parallel()

	const goroutines = 16

	var waitGroup sync.WaitGroup

	errs := make([]error, goroutines)

	for i := range goroutines {
		waitGroup.Go(func() {
			cmd := newSilentCmd(func(_ *cobra.Command, _ []string) {
				cmdutil.CheckErr(errBoom)
			})
			errs[i] = kubectl.ExecuteSafely(context.Background(), cmd)
		})
	}

	waitGroup.Wait()

	for i, err := range errs {
		require.Errorf(t, err, "goroutine %d expected a recovered fatal error", i)
	}
}
