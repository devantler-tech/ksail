package kubernetes_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/kubernetes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errSentinelWait = errors.New("not ready")

func TestMapWaitErrorConditionErrorPropagatesRaw(t *testing.T) {
	t.Parallel()

	// A condition (Get) error must be returned verbatim, even when it wraps a
	// context error — it must not be mistaken for the poller's own cancellation.
	condErr := fmt.Errorf("get Gateway: %w", context.Canceled)

	got := kubernetes.MapWaitErrorForTest(condErr, condErr, "waiting for Gateway", errSentinelWait)

	require.ErrorIs(t, got, condErr)
	require.NotErrorIs(t, got, errSentinelWait)
	assert.NotContains(t, got.Error(), "waiting for Gateway")
}

func TestMapWaitErrorRealCancellationWrapsCause(t *testing.T) {
	t.Parallel()

	// The poller's own cancellation (no condition error captured) maps to the
	// "<cause>: <ctx err>" message.
	got := kubernetes.MapWaitErrorForTest(
		context.Canceled,
		nil,
		"waiting for Gateway",
		errSentinelWait,
	)

	require.ErrorIs(t, got, context.Canceled)
	assert.Contains(t, got.Error(), "waiting for Gateway")
}

func TestMapWaitErrorTimeoutReturnsSentinel(t *testing.T) {
	t.Parallel()

	got := kubernetes.MapWaitErrorForTest(
		context.DeadlineExceeded,
		nil,
		"waiting for Gateway",
		errSentinelWait,
	)

	require.ErrorIs(t, got, errSentinelWait)
}
