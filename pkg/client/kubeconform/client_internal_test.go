package kubeconform

import (
	"context"
	"errors"
	"testing"
	"time"
)

// Static sentinel errors for the retry tests (err113: avoid inline dynamic errors).
//

var (
	// errTransientEOF mimics a truncated schema download (netretry-retryable).
	errTransientEOF = errors.New("unexpected EOF")
	// errTransientReset mimics a dropped connection (netretry-retryable).
	errTransientReset = errors.New("connection reset by peer")
	// errNonTransient mimics a real schema-validation failure (not retryable).
	errNonTransient = errors.New("schema invalid: required field missing")
)

// newFastRetryClient returns a client with negligible backoff so retry tests
// stay fast. Each test owns its own client, so mutating these fields is
// parallel-safe (no shared global state).
func newFastRetryClient() *Client {
	client := NewClient()
	client.retryBaseWait = time.Millisecond
	client.retryMaxWait = time.Millisecond

	return client
}

func TestValidateWithRetrySucceedsAfterTransientError(t *testing.T) {
	t.Parallel()

	client := newFastRetryClient()
	calls := 0

	err := client.validateWithRetry(context.Background(), func() error {
		calls++
		if calls < 2 {
			return errTransientEOF
		}

		return nil
	})
	if err != nil {
		t.Fatalf("expected success after a transient retry, got error: %v", err)
	}

	if calls != 2 {
		t.Fatalf("expected 2 attempts, got %d", calls)
	}
}

func TestValidateWithRetryDoesNotRetryNonTransientError(t *testing.T) {
	t.Parallel()

	client := newFastRetryClient()
	calls := 0

	err := client.validateWithRetry(context.Background(), func() error {
		calls++

		return errNonTransient
	})
	if !errors.Is(err, errNonTransient) {
		t.Fatalf("expected the original error, got: %v", err)
	}

	if calls != 1 {
		t.Fatalf("expected 1 attempt for a non-transient error, got %d", calls)
	}
}

func TestValidateWithRetryGivesUpAfterMaxAttempts(t *testing.T) {
	t.Parallel()

	client := newFastRetryClient()
	calls := 0

	err := client.validateWithRetry(context.Background(), func() error {
		calls++

		return errTransientReset
	})
	if err == nil {
		t.Fatal("expected an error after exhausting all attempts")
	}

	if calls != client.maxRetryAttempts {
		t.Fatalf("expected %d attempts, got %d", client.maxRetryAttempts, calls)
	}
}

func TestValidateWithRetryStopsOnContextCancel(t *testing.T) {
	t.Parallel()

	// Use real (non-trivial) backoff so the cancel is observed during the wait.
	client := NewClient()
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0

	err := client.validateWithRetry(ctx, func() error {
		calls++

		cancel()

		return errTransientEOF
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got: %v", err)
	}

	if calls != 1 {
		t.Fatalf("expected 1 attempt before cancellation, got %d", calls)
	}
}

func TestValidateWithRetryTreatsZeroAttemptsAsOne(t *testing.T) {
	t.Parallel()

	client := NewClient()
	client.maxRetryAttempts = 0
	calls := 0

	err := client.validateWithRetry(context.Background(), func() error {
		calls++

		return errTransientEOF
	})
	if err == nil {
		t.Fatal("expected an error")
	}

	if calls != 1 {
		t.Fatalf("expected exactly 1 attempt, got %d", calls)
	}
}
