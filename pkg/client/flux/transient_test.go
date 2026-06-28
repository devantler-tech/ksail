package flux_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/client/flux"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// Static sentinel errors for the table-driven cases (err113 forbids inline
// errors.New in tests). The transient classifiers match on the wrapped status
// type or the rendered message, so these sentinels exercise the message paths.
var (
	errConflict             = errors.New("conflict")
	errAPIDiscoveryNotFound = errors.New("the server could not find the requested resource")
	errNoMatchesForKind     = errors.New(
		"no matches for kind \"OCIRepository\" in version \"source.toolkit.fluxcd.io/v1\"",
	)
	errConnectionRefused = errors.New("dial tcp: connection refused")
	errPermanent         = errors.New("validation failed: invalid spec")
	errResourceNotReady  = errors.New("resource not found yet")
)

func TestIsConnectionError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		errMsg string
		want   bool
	}{
		{
			name:   "connection refused",
			errMsg: "dial tcp 127.0.0.1:6443: connect: connection refused",
			want:   true,
		},
		{name: "connection reset", errMsg: "read tcp: connection reset", want: true},
		{name: "i/o timeout", errMsg: "dial tcp: i/o timeout", want: true},
		{name: "EOF", errMsg: "unexpected response: EOF", want: true},
		{name: "unrelated error", errMsg: "the requested operation is not permitted", want: false},
		{name: "empty string", errMsg: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, flux.IsConnectionError(tt.errMsg))
		})
	}
}

func TestIsTransientAPIError(t *testing.T) {
	t.Parallel()

	groupResource := schema.GroupResource{
		Group:    "source.toolkit.fluxcd.io",
		Resource: "ocirepositories",
	}

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil error", err: nil, want: false},
		{
			name: "service unavailable",
			err:  apierrors.NewServiceUnavailable("flux not ready"),
			want: true,
		},
		{name: "timeout", err: apierrors.NewTimeoutError("request timed out", 1), want: true},
		{name: "too many requests", err: apierrors.NewTooManyRequests("slow down", 1), want: true},
		{
			name: "conflict",
			err:  apierrors.NewConflict(groupResource, "flux-system", errConflict),
			want: true,
		},
		{name: "not found", err: apierrors.NewNotFound(groupResource, "flux-system"), want: true},
		{name: "api discovery not found substring", err: errAPIDiscoveryNotFound, want: true},
		{name: "api discovery no matches for kind", err: errNoMatchesForKind, want: true},
		{name: "connection error message", err: errConnectionRefused, want: true},
		{name: "permanent non-transient error", err: errPermanent, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tt.want, flux.IsTransientAPIError(tt.err))
		})
	}
}

func TestTimeoutWaitingError(t *testing.T) {
	t.Parallel()

	ctxErr := context.DeadlineExceeded

	err := flux.TimeoutWaitingError("OCIRepository flux-system", errResourceNotReady, ctxErr)

	require.Error(t, err)
	assert.Contains(
		t,
		err.Error(),
		"timed out waiting for OCIRepository flux-system to be available",
	)
	// The message wraps both the last transient error and the context error with %w.
	require.ErrorIs(t, err, errResourceNotReady)
	require.ErrorIs(t, err, ctxErr)
}

func TestHandleTransientError_ContextDone(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Done() fires immediately.

	// A long interval guarantees the ticker never wins the select race.
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()

	err := flux.HandleTransientError(ctx, ticker, "Kustomization flux-system", errResourceNotReady)

	require.Error(t, err)
	require.ErrorIs(t, err, context.Canceled)
	require.ErrorIs(t, err, errResourceNotReady)
	assert.Contains(
		t,
		err.Error(),
		"timed out waiting for Kustomization flux-system to be available",
	)
}

func TestHandleTransientError_TickerFires(t *testing.T) {
	t.Parallel()

	// context.Background never reports Done, so the select can only proceed
	// once the ticker fires, returning nil to continue the retry loop.
	ctx := context.Background()

	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()

	err := flux.HandleTransientError(ctx, ticker, "Kustomization flux-system", errResourceNotReady)

	require.NoError(t, err)
}
