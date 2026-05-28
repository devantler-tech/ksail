package hetzner_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provider"
	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/hetzner"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	// retryTestServerType is the server type served by the counting test server.
	retryTestServerType = "cx22"
	// retryTestLocation is the primary location used across retry tests.
	retryTestLocation = "fsn1"
)

// noDelay is a delay function that never sleeps, keeping retry tests fast.
func noDelay(int) time.Duration { return 0 }

// newCountingAvailabilityServer returns a test server that reports
// retryTestServerType as available only once it has been queried at least
// availableAfter times. Every call increments calls so tests can assert how many
// attempts were made. When availableAfter is 0 the type is never reported available.
func newCountingAvailabilityServer(
	t *testing.T,
	availableAfter int32,
	calls *atomic.Int32,
) *httptest.Server {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc("GET /server_types", func(writer http.ResponseWriter, r *http.Request) {
		count := calls.Add(1)
		available := availableAfter > 0 && count >= availableAfter

		resp := serverTypeListResp{}
		if r.URL.Query().Get("name") == retryTestServerType {
			resp.ServerTypes = []serverTypeSchema{
				{ID: 1, Name: retryTestServerType, Locations: []serverTypeLocSchema{
					{ID: 1, Name: retryTestLocation, Available: available},
				}},
			}
		}

		writeJSONResponse(t, writer, resp)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	return srv
}

func TestCheckServerAvailabilityWithRetry_SucceedsAfterRetry(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32

	srv := newCountingAvailabilityServer(t, 3, &calls)
	prov := hetzner.NewProvider(newTestHcloudClient(srv.URL))

	err := prov.CheckServerAvailabilityWithRetryForTest(
		context.Background(),
		[]string{retryTestServerType},
		retryTestLocation,
		nil,
		5,
		io.Discard,
		noDelay,
	)

	require.NoError(t, err)
	assert.Equal(t, int32(3), calls.Load(), "should retry until available on the third attempt")
}

func TestCheckServerAvailabilityWithRetry_ExhaustsRetries(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32

	// availableAfter 0 => never available, so every attempt fails.
	srv := newCountingAvailabilityServer(t, 0, &calls)
	prov := hetzner.NewProvider(newTestHcloudClient(srv.URL))

	err := prov.CheckServerAvailabilityWithRetryForTest(
		context.Background(),
		[]string{retryTestServerType},
		retryTestLocation,
		nil,
		3,
		io.Discard,
		noDelay,
	)

	require.ErrorIs(t, err, hetzner.ErrServerTypeUnavailable)
	assert.Equal(t, int32(3), calls.Load(), "should attempt exactly maxAttempts times")
}

func TestCheckServerAvailabilityWithRetry_PermanentErrorNotRetried(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32

	// The queried type is never returned, so the lookup yields ErrServerTypeNotFound.
	srv := newCountingAvailabilityServer(t, 1, &calls)
	prov := hetzner.NewProvider(newTestHcloudClient(srv.URL))

	err := prov.CheckServerAvailabilityWithRetryForTest(
		context.Background(),
		[]string{"nonexistent"},
		retryTestLocation,
		nil,
		5,
		io.Discard,
		noDelay,
	)

	require.ErrorIs(t, err, hetzner.ErrServerTypeNotFound)
	assert.Equal(t, int32(1), calls.Load(), "permanent errors must not be retried")
}

func TestCheckServerAvailabilityWithRetry_ContextCancelledDuringWait(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32

	srv := newCountingAvailabilityServer(t, 0, &calls)
	prov := hetzner.NewProvider(newTestHcloudClient(srv.URL))

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel the context during the backoff wait of the first attempt.
	cancelDuringDelay := func(int) time.Duration {
		cancel()

		return time.Hour
	}

	err := prov.CheckServerAvailabilityWithRetryForTest(
		ctx,
		[]string{retryTestServerType},
		retryTestLocation,
		nil,
		5,
		io.Discard,
		cancelDuringDelay,
	)

	require.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, int32(1), calls.Load(), "should stop after the cancelled backoff")
}

func TestCheckServerAvailabilityWithRetry_ClampsNonPositiveMaxAttempts(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32

	srv := newCountingAvailabilityServer(t, 0, &calls)
	prov := hetzner.NewProvider(newTestHcloudClient(srv.URL))

	err := prov.CheckServerAvailabilityWithRetryForTest(
		context.Background(),
		[]string{retryTestServerType},
		retryTestLocation,
		nil,
		0,
		io.Discard,
		noDelay,
	)

	require.ErrorIs(t, err, hetzner.ErrServerTypeUnavailable)
	assert.Equal(t, int32(1), calls.Load(), "maxAttempts < 1 should clamp to a single attempt")
}

func TestCheckServerAvailabilityWithRetry_PublicWrapperSucceeds(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32

	srv := newCountingAvailabilityServer(t, 1, &calls)
	prov := hetzner.NewProvider(newTestHcloudClient(srv.URL))

	// maxAttempts of 1 means the public wrapper never sleeps even though it uses
	// the real backoff delay function.
	err := prov.CheckServerAvailabilityWithRetry(
		context.Background(),
		[]string{retryTestServerType},
		retryTestLocation,
		nil,
		1,
		io.Discard,
	)

	require.NoError(t, err)
	assert.Equal(t, int32(1), calls.Load())
}

func TestCheckServerAvailabilityWithRetry_NilClient(t *testing.T) {
	t.Parallel()

	prov := hetzner.NewProvider(nil)

	err := prov.CheckServerAvailabilityWithRetry(
		context.Background(),
		[]string{retryTestServerType},
		retryTestLocation,
		nil,
		hetzner.DefaultMaxAvailabilityCheckRetries,
		io.Discard,
	)

	require.ErrorIs(t, err, provider.ErrProviderUnavailable)
}

func TestIsRetryableAvailabilityError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "Nil", err: nil, want: false},
		{name: "ServerTypeUnavailable", err: hetzner.ErrServerTypeUnavailable, want: true},
		{
			name: "WrappedServerTypeUnavailable",
			err:  fmt.Errorf("check: %w", hetzner.ErrServerTypeUnavailable),
			want: true,
		},
		{name: "ServerTypeNotFound", err: hetzner.ErrServerTypeNotFound, want: false},
		{
			name: "RetryableHcloudError",
			err:  hcloud.Error{Code: hcloud.ErrorCodeResourceUnavailable},
			want: true,
		},
		{
			name: "WrappedRateLimit",
			err:  fmt.Errorf("lookup: %w", hcloud.Error{Code: hcloud.ErrorCodeRateLimitExceeded}),
			want: true,
		},
		{
			name: "ForbiddenNotRetryable",
			err:  hcloud.Error{Code: hcloud.ErrorCodeForbidden},
			want: false,
		},
		{name: "ProviderUnavailable", err: provider.ErrProviderUnavailable, want: false},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := hetzner.IsRetryableAvailabilityErrorForTest(testCase.err)
			assert.Equal(t, testCase.want, got)
		})
	}
}
