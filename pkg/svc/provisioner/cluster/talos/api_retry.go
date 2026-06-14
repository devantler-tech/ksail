package talosprovisioner

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/client/netretry"
	talosclient "github.com/siderolabs/talos/pkg/machinery/client"
)

// Retry defaults for transient per-node Talos API failures. A single TLS
// handshake to apid (:50000) can stall transiently (e.g. a per-flow network
// drop between a CI runner and a cloud node's public IP) and fail with
// "authentication handshake failed: context deadline exceeded" after gRPC's
// ~20s connect timeout, even though the node is healthy. Bounded retries with
// a fresh client per attempt prevent one flaky handshake from failing a whole
// cluster operation.
const (
	defaultTalosAPIRetryMaxAttempts = 3
	defaultTalosAPIRetryBaseWait    = 5 * time.Second
	defaultTalosAPIRetryMaxWait     = 20 * time.Second

	// grpcUnavailable and grpcDeadlineExceeded are the numeric gRPC status
	// codes for Unavailable (14) and DeadlineExceeded (4). The raw constants
	// are used because depguard forbids importing google.golang.org/grpc from
	// production code; talosclient.StatusCode returns an equivalent codes.Code.
	grpcUnavailable      = 14
	grpcDeadlineExceeded = 4
)

// errRetriesExhausted is returned when all retry attempts for a Talos API call
// have been used.
var errRetriesExhausted = errors.New("retries exhausted")

// talosAPIRetryConfig holds retry parameters for per-node Talos API calls.
type talosAPIRetryConfig struct {
	maxAttempts int
	baseWait    time.Duration
	maxWait     time.Duration
}

// defaultTalosAPIRetryConfig returns the default retry configuration for
// per-node Talos API calls.
func defaultTalosAPIRetryConfig() talosAPIRetryConfig {
	return talosAPIRetryConfig{
		maxAttempts: defaultTalosAPIRetryMaxAttempts,
		baseWait:    defaultTalosAPIRetryBaseWait,
		maxWait:     defaultTalosAPIRetryMaxWait,
	}
}

// retryTransientTalosAPICall runs operation with bounded retries and
// exponential backoff for transient gRPC failures (Unavailable,
// DeadlineExceeded). Each attempt must create its own Talos client inside
// operation so a fresh connection is dialed. Non-transient errors and
// parent-context cancellation fail
// immediately; once attempts are exhausted the last error is returned wrapped
// in errRetriesExhausted. target names the node and description the operation
// for retry log lines.
func (p *Provisioner) retryTransientTalosAPICall(
	ctx context.Context,
	target, description string,
	operation func(ctx context.Context) error,
) error {
	cfg := p.talosAPIRetry
	if cfg.maxAttempts <= 0 {
		cfg = defaultTalosAPIRetryConfig()
	}

	var lastErr error

	for attempt := 1; attempt <= cfg.maxAttempts; attempt++ {
		lastErr = operation(ctx)
		if lastErr == nil {
			return nil
		}

		if !isTransientTalosAPIError(lastErr) || ctx.Err() != nil {
			return lastErr
		}

		if attempt == cfg.maxAttempts {
			break
		}

		delay := netretry.ExponentialDelay(attempt, cfg.baseWait, cfg.maxWait)

		p.logf(
			"  %s attempt %d/%d failed on %s (retrying in %s): %v\n",
			description,
			attempt,
			cfg.maxAttempts,
			target,
			delay,
			lastErr,
		)

		sleepErr := sleepWithContext(ctx, delay)
		if sleepErr != nil {
			return fmt.Errorf("retry backoff interrupted: %w", sleepErr)
		}
	}

	return fmt.Errorf("%w: %w", errRetriesExhausted, lastErr)
}

// sleepWithContext waits for d to elapse, returning ctx.Err() early if the context is cancelled.
func sleepWithContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	select {
	case <-ctx.Done():
		if !timer.Stop() {
			<-timer.C
		}

		return fmt.Errorf("%w", ctx.Err())
	case <-timer.C:
		return nil
	}
}

// isTransientTalosAPIError reports whether err indicates a transient Talos API
// failure worth retrying: gRPC Unavailable/DeadlineExceeded, or text markers of
// connection-level handshake/timeout failures. Callers must still stop retrying
// when their own context is done, since a parent-context deadline also surfaces
// as "context deadline exceeded".
func isTransientTalosAPIError(err error) bool {
	if err == nil {
		return false
	}

	code := talosclient.StatusCode(err)
	if code == grpcUnavailable || code == grpcDeadlineExceeded {
		return true
	}

	errMsg := strings.ToLower(err.Error())

	return strings.Contains(errMsg, "rpc error: code = unavailable") ||
		strings.Contains(errMsg, "rpc error: code = deadlineexceeded") ||
		strings.Contains(errMsg, "authentication handshake failed") ||
		strings.Contains(errMsg, "context deadline exceeded")
}
