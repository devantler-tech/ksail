package helm_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/client/helm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// errTransientLocate is a transient network error string the default netretry
// predicate (IsRetryable) classifies as retryable.
var errTransientLocate = errors.New(
	"failed to fetch index: Get \"https://charts.example.com/index.yaml\": " +
		"read tcp 10.0.0.1:54321->1.2.3.4:443: read: connection reset by peer",
)

// errPermanentLocate is a non-network error the default predicate does NOT retry
// (e.g. a real 404 / chart-not-found), so the locate must fail on the first try.
var errPermanentLocate = errors.New("chart \"missing\" version \"1.0.0\" not found")

const (
	retryTestBaseWait = time.Millisecond
	retryTestMaxWait  = time.Millisecond
)

// TestLocateChartWithRetry_SuccessFirstAttempt verifies the happy path: a locate
// that succeeds immediately is called exactly once and returns its path.
func TestLocateChartWithRetry_SuccessFirstAttempt(t *testing.T) {
	t.Parallel()

	calls := 0

	path, err := helm.LocateChartWithRetry(
		context.Background(),
		5,
		retryTestBaseWait,
		retryTestMaxWait,
		func() (string, error) {
			calls++

			return "/cache/chart.tgz", nil
		},
	)

	require.NoError(t, err)
	assert.Equal(t, "/cache/chart.tgz", path)
	assert.Equal(t, 1, calls, "successful locate must not retry")
}

// TestLocateChartWithRetry_RetriesTransientThenSucceeds is the core #5371
// regression guard: a cold-cache fetch that fails twice with a transient network
// error must be retried and ultimately succeed, instead of degrading the render.
func TestLocateChartWithRetry_RetriesTransientThenSucceeds(t *testing.T) {
	t.Parallel()

	calls := 0

	path, err := helm.LocateChartWithRetry(
		context.Background(),
		5,
		retryTestBaseWait,
		retryTestMaxWait,
		func() (string, error) {
			calls++
			if calls < 3 {
				return "", errTransientLocate
			}

			return "/cache/chart.tgz", nil
		},
	)

	require.NoError(t, err)
	assert.Equal(t, "/cache/chart.tgz", path)
	assert.Equal(t, 3, calls, "transient failures must be retried until success")
}

// TestLocateChartWithRetry_StopsOnNonRetryable verifies a permanent error (e.g.
// chart-not-found) is not retried, so a genuine misconfiguration still fails fast.
func TestLocateChartWithRetry_StopsOnNonRetryable(t *testing.T) {
	t.Parallel()

	calls := 0

	_, err := helm.LocateChartWithRetry(
		context.Background(),
		5,
		retryTestBaseWait,
		retryTestMaxWait,
		func() (string, error) {
			calls++

			return "", errPermanentLocate
		},
	)

	require.Error(t, err)
	require.ErrorIs(t, err, errPermanentLocate)
	assert.Equal(t, 1, calls, "non-retryable error must not be retried")
}

// TestLocateChartWithRetry_ExhaustsRetries verifies that a persistently transient
// failure is retried up to the attempt budget and then returns the last error.
func TestLocateChartWithRetry_ExhaustsRetries(t *testing.T) {
	t.Parallel()

	calls := 0

	_, err := helm.LocateChartWithRetry(
		context.Background(),
		3,
		retryTestBaseWait,
		retryTestMaxWait,
		func() (string, error) {
			calls++

			return "", errTransientLocate
		},
	)

	require.Error(t, err)
	require.ErrorIs(t, err, errTransientLocate)
	assert.Equal(t, 3, calls, "must attempt exactly the configured number of times")
}
