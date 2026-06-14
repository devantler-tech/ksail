package talosprovisioner_test

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"testing"
	"time"

	talosprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/talos"
	talosclient "github.com/siderolabs/talos/pkg/machinery/client"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// newRetryProvisioner builds a provisioner whose transient-retry loop uses
// near-zero delays and discards retry log output, so the retry behavior can be
// unit tested without real Talos connectivity or slow backoff sleeps.
func newRetryProvisioner(maxAttempts int) *talosprovisioner.Provisioner {
	return talosprovisioner.NewProvisioner(nil, talosprovisioner.NewOptions()).
		WithLogWriter(io.Discard).
		WithTalosAPIRetryConfig(maxAttempts, time.Millisecond, time.Millisecond)
}

// newClientErrProvisioner builds a provisioner whose createTalosClient fails
// deterministically and offline: no in-memory talosConfigs bundle and a
// talosconfig path that does not exist, so client creation returns a
// non-transient error before any network I/O.
func newClientErrProvisioner(t *testing.T) *talosprovisioner.Provisioner {
	t.Helper()

	missingPath := filepath.Join(t.TempDir(), "missing-talosconfig")

	return talosprovisioner.NewProvisioner(
		nil,
		talosprovisioner.NewOptions().WithTalosconfigPath(missingPath),
	).
		WithLogWriter(io.Discard).
		WithTalosAPIRetryConfig(3, time.Millisecond, time.Millisecond)
}

func TestRetryTransientTalosAPICall_SucceedsFirstAttempt(t *testing.T) {
	t.Parallel()

	prov := newRetryProvisioner(3)
	calls := 0

	err := prov.RetryTransientTalosAPICallForTest(
		context.Background(), "1.2.3.4", "probe",
		func() error {
			calls++

			return nil
		},
	)
	if err != nil {
		t.Fatalf("RetryTransientTalosAPICallForTest() = %v, want nil", err)
	}

	if calls != 1 {
		t.Fatalf("operation called %d times, want 1", calls)
	}
}

func TestRetryTransientTalosAPICall_RetriesThenSucceeds(t *testing.T) {
	t.Parallel()

	prov := newRetryProvisioner(3)
	calls := 0

	err := prov.RetryTransientTalosAPICallForTest(
		context.Background(), "1.2.3.4", "probe",
		func() error {
			calls++
			if calls < 3 {
				return errUnavailableMessage
			}

			return nil
		},
	)
	if err != nil {
		t.Fatalf("RetryTransientTalosAPICallForTest() = %v, want nil", err)
	}

	if calls != 3 {
		t.Fatalf("operation called %d times, want 3", calls)
	}
}

func TestRetryTransientTalosAPICall_ExhaustsAttempts(t *testing.T) {
	t.Parallel()

	prov := newRetryProvisioner(3)
	calls := 0

	err := prov.RetryTransientTalosAPICallForTest(
		context.Background(), "1.2.3.4", "probe",
		func() error {
			calls++

			return errUnavailableMessage
		},
	)
	if err == nil {
		t.Fatal("RetryTransientTalosAPICallForTest() = nil, want error")
	}

	if calls != 3 {
		t.Fatalf("operation called %d times, want 3 (maxAttempts)", calls)
	}

	if !errors.Is(err, talosprovisioner.ErrRetriesExhausted) {
		t.Fatalf("error %v does not wrap ErrRetriesExhausted", err)
	}

	if !errors.Is(err, errUnavailableMessage) {
		t.Fatalf("error %v does not wrap the underlying transient error", err)
	}
}

func TestRetryTransientTalosAPICall_NonRetryableReturnedImmediately(t *testing.T) {
	t.Parallel()

	prov := newRetryProvisioner(3)
	calls := 0

	err := prov.RetryTransientTalosAPICallForTest(
		context.Background(), "1.2.3.4", "probe",
		func() error {
			calls++

			return errPermissionDenied
		},
	)
	if !errors.Is(err, errPermissionDenied) {
		t.Fatalf("RetryTransientTalosAPICallForTest() = %v, want errPermissionDenied", err)
	}

	if calls != 1 {
		t.Fatalf("operation called %d times, want 1 (no retry on non-retryable)", calls)
	}

	if errors.Is(err, talosprovisioner.ErrRetriesExhausted) {
		t.Fatal("non-retryable error must not be wrapped with ErrRetriesExhausted")
	}
}

func TestRetryTransientTalosAPICall_ContextCancelledDuringBackoff(t *testing.T) {
	t.Parallel()

	prov := newRetryProvisioner(3)
	calls := 0

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel up-front so the first backoff sleep is interrupted

	err := prov.RetryTransientTalosAPICallForTest(
		ctx, "1.2.3.4", "probe",
		func() error {
			calls++

			return errUnavailableMessage
		},
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("RetryTransientTalosAPICallForTest() = %v, want context.Canceled", err)
	}

	if calls != 1 {
		t.Fatalf("operation called %d times, want 1 (backoff interrupted before retry)", calls)
	}
}

func TestRetryTransientTalosAPICall_ZeroMaxAttemptsRunsOnce(t *testing.T) {
	t.Parallel()

	// A misconfigured (or zero-value) retry config must still run the operation
	// exactly once rather than skipping it entirely.
	prov := newRetryProvisioner(0)
	calls := 0

	err := prov.RetryTransientTalosAPICallForTest(
		context.Background(), "1.2.3.4", "probe",
		func() error {
			calls++

			return errUnavailableMessage
		},
	)
	if err == nil {
		t.Fatal("RetryTransientTalosAPICallForTest() = nil, want error")
	}

	if calls != 1 {
		t.Fatalf("operation called %d times, want 1", calls)
	}
}

func TestWithTalosClient_ClientCreationErrorShortCircuits(t *testing.T) {
	t.Parallel()

	prov := newClientErrProvisioner(t)
	operationCalls := 0

	err := prov.WithTalosClientForTest(
		context.Background(), "1.2.3.4", "probe",
		func(*talosclient.Client) error {
			operationCalls++

			return nil
		},
	)
	if err == nil {
		t.Fatal("WithTalosClientForTest() = nil, want a client-creation error")
	}

	if operationCalls != 0 {
		t.Fatalf("operation invoked %d times, want 0 (client creation failed)", operationCalls)
	}

	// A client-creation/config error is not transient, so it must not be retried
	// to exhaustion — it should surface immediately.
	if errors.Is(err, talosprovisioner.ErrRetriesExhausted) {
		t.Fatal("non-retryable client-creation error must not be wrapped with ErrRetriesExhausted")
	}
}

func TestDialTalosClientWithRetry_ClientCreationErrorShortCircuits(t *testing.T) {
	t.Parallel()

	prov := newClientErrProvisioner(t)

	client, err := prov.DialTalosClientWithRetryForTest(context.Background(), "1.2.3.4", "probe")
	if err == nil {
		t.Fatal("DialTalosClientWithRetryForTest() err = nil, want a client-creation error")
	}

	if client != nil {
		t.Fatalf("DialTalosClientWithRetryForTest() client = %v, want nil", client)
	}

	if errors.Is(err, talosprovisioner.ErrRetriesExhausted) {
		t.Fatal("non-retryable client-creation error must not be wrapped with ErrRetriesExhausted")
	}
}

func TestIsRetryableTransientTalosError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "NilError", err: nil, want: false},
		{name: "GRPCUnavailable", err: status.Error(codes.Unavailable, "x"), want: true},
		{name: "GRPCDeadlineExceeded", err: status.Error(codes.DeadlineExceeded, "x"), want: true},
		{name: "UnavailableMessage", err: errUnavailableMessage, want: true},
		{name: "HandshakeFailedMessage", err: errHandshakeFailed, want: true},
		{name: "NonRetryableGrpcError", err: status.Error(codes.InvalidArgument, "x"), want: false},
		{name: "NonRetryableError", err: errPermissionDenied, want: false},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := talosprovisioner.IsRetryableTransientTalosError(testCase.err)
			if got != testCase.want {
				t.Fatalf("IsRetryableTransientTalosError(%v) = %v, want %v",
					testCase.err, got, testCase.want)
			}
		})
	}
}
