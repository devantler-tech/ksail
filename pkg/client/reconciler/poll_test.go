package reconciler_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/client/reconciler"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testPollInterval = time.Millisecond

var (
	errPermanentPoll = errors.New("boom")
	errHaltPoll      = errors.New("dependency failed")
	errTimeoutPoll   = errors.New("timed out")
)

func timeoutErr(lastStatus string) error {
	if lastStatus != "" {
		return fmt.Errorf("%w (last status: %s)", errTimeoutPoll, lastStatus)
	}

	return errTimeoutPoll
}

func TestPollUntilReadyReadyImmediately(t *testing.T) {
	t.Parallel()

	err := reconciler.PollUntilReady(
		t.Context(),
		testPollInterval,
		func(context.Context) (reconciler.CheckResult, error) {
			return reconciler.CheckResult{Ready: true}, nil
		},
		timeoutErr,
	)

	require.NoError(t, err)
}

func TestPollUntilReadyPermanentFailureWrapped(t *testing.T) {
	t.Parallel()

	err := reconciler.PollUntilReady(
		t.Context(),
		testPollInterval,
		func(context.Context) (reconciler.CheckResult, error) {
			return reconciler.CheckResult{}, errPermanentPoll
		},
		timeoutErr,
	)

	require.ErrorIs(t, err, errPermanentPoll)
	assert.Contains(t, err.Error(), "permanent failure")
}

func TestPollUntilReadyHaltReturnsVerbatim(t *testing.T) {
	t.Parallel()

	err := reconciler.PollUntilReady(
		t.Context(),
		testPollInterval,
		func(context.Context) (reconciler.CheckResult, error) {
			return reconciler.CheckResult{}, reconciler.Halt(errHaltPoll)
		},
		timeoutErr,
	)

	require.ErrorIs(t, err, errHaltPoll)
	assert.NotContains(t, err.Error(), "permanent failure")
}

func TestPollUntilReadyCancelPropagates(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := reconciler.PollUntilReady(
		ctx,
		testPollInterval,
		func(ctx context.Context) (reconciler.CheckResult, error) {
			// Simulate a client returning the context's cancellation error.
			return reconciler.CheckResult{}, ctx.Err()
		},
		timeoutErr,
	)

	require.ErrorIs(t, err, context.Canceled)
	assert.NotContains(t, err.Error(), "permanent failure")
}

func TestPollUntilReadyTimeoutUsesLastStatus(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	err := reconciler.PollUntilReady(
		ctx,
		testPollInterval,
		func(ctx context.Context) (reconciler.CheckResult, error) {
			if ctx.Err() != nil {
				// Surface the deadline as a context error from the check.
				return reconciler.CheckResult{}, ctx.Err()
			}

			return reconciler.CheckResult{Ready: false, Status: "Progressing"}, nil
		},
		timeoutErr,
	)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "last status: Progressing")
}
