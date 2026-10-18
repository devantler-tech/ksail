package hetzner_test

import (
	"context"
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/svc/provider/hetzner"
	"github.com/hetznercloud/hcloud-go/v2/hcloud"
	"github.com/stretchr/testify/require"
)

// statusWaitTransport replaces only the remote HTTP boundary of the real status poll.
type statusWaitTransport func(*http.Request) (*http.Response, error)

// RoundTrip handles one status request without contacting Hetzner.
func (transport statusWaitTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

type statusWaitCase struct {
	name            string
	cancelPoll      bool
	waitForDeadline bool
	requestError    error
	wantError       error
	wantTimeout     bool
}

// client interrupts a real status request at the selected context boundary.
func (testCase statusWaitCase) client(
	cancel context.CancelFunc,
	requestStarted *atomic.Bool,
) *hcloud.Client {
	transport := statusWaitTransport(func(request *http.Request) (*http.Response, error) {
		requestStarted.Store(true)

		if testCase.cancelPoll {
			cancel()
		}

		if (testCase.cancelPoll && testCase.requestError == nil) || testCase.waitForDeadline {
			<-request.Context().Done()

			return nil, fmt.Errorf("status request interrupted: %w", request.Context().Err())
		}

		return nil, fmt.Errorf("independent status request failure: %w", testCase.requestError)
	})

	return hcloud.NewClient(
		hcloud.WithToken("test-token"),
		hcloud.WithHTTPClient(&http.Client{Transport: transport}),
	)
}

// checkError verifies the caller-visible error and its original context cause.
func (testCase statusWaitCase) checkError(t *testing.T, err, contextErr error) {
	t.Helper()
	require.ErrorIs(t, err, testCase.wantError)

	if testCase.wantTimeout {
		require.ErrorContains(t, err, "timeout waiting for servers to reach status off")
	} else {
		if testCase.cancelPoll {
			require.ErrorIs(t, contextErr, context.Canceled)
		} else {
			require.NoError(
				t,
				contextErr,
				"the poll must remain live for an independent request failure",
			)
		}

		require.ErrorContains(t, err, "failed to list servers")
		require.NotContains(t, err.Error(), "timeout waiting for servers")
	}
}

func TestWaitForServersStatus_ContextErrorAttribution(t *testing.T) {
	t.Parallel()

	tests := []statusWaitCase{
		{
			name:            "deadline cancels in-flight status request",
			waitForDeadline: true,
			wantError:       context.DeadlineExceeded,
			wantTimeout:     true,
		},
		{
			name:        "cancellation cancels in-flight status request",
			cancelPoll:  true,
			wantError:   context.Canceled,
			wantTimeout: true,
		},
		{
			name:         "independent request deadline keeps API failure",
			requestError: context.DeadlineExceeded,
			wantError:    context.DeadlineExceeded,
		},
		{
			name:         "independent request cancellation keeps API failure",
			requestError: context.Canceled,
			wantError:    context.Canceled,
		},
		{
			name:         "independent API failure racing with poll cancellation is preserved",
			cancelPoll:   true,
			requestError: http.ErrHandlerTimeout,
			wantError:    http.ErrHandlerTimeout,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithTimeout(
				t.Context(),
				hetzner.DefaultPollingInterval+10*time.Second,
			)
			defer cancel()

			var requestStarted atomic.Bool

			prov := hetzner.NewProvider(testCase.client(cancel, &requestStarted))

			err := prov.WaitForServersStatusForTest(ctx, "test-cluster", hcloud.ServerStatusOff)

			require.True(
				t,
				requestStarted.Load(),
				"the deadline must interrupt an actual status request",
			)
			testCase.checkError(t, err, ctx.Err())
		})
	}
}
