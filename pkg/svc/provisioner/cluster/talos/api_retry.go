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

// gRPC status codes for the transient per-node Talos (apid) failures KSail
// retries. The raw numeric values are used instead of importing
// google.golang.org/grpc/codes: depguard forbids depending on
// google.golang.org/grpc in production code, and talosclient.StatusCode returns
// a value comparable to these. This file is the single home for the
// transient-retry policy, so the constants live here.
const (
	// grpcUnavailable is the numeric gRPC status code for Unavailable (codes.Unavailable).
	grpcUnavailable = 14
	// grpcDeadlineExceeded is the numeric gRPC status code for DeadlineExceeded (codes.DeadlineExceeded).
	grpcDeadlineExceeded = 4
)

// Default bounded-retry policy for transient per-node Talos API calls. A freshly
// reachable node's apid (:50000) often drops the first TLS handshake; it
// typically clears within a few seconds, so a small number of attempts with
// exponential backoff is sufficient. These mirror the apply-config retry
// defaults so all per-node Talos retries behave consistently.
const (
	defaultTalosAPIMaxAttempts   = 3
	defaultTalosAPIRetryBaseWait = 5 * time.Second
	defaultTalosAPIRetryMaxWait  = 20 * time.Second
)

// talosAPIRetryConfig holds the bounded-retry parameters for transient per-node
// Talos gRPC calls. Tests override it via WithTalosAPIRetryConfig to use
// near-zero delays.
type talosAPIRetryConfig struct {
	maxAttempts int
	baseWait    time.Duration
	maxWait     time.Duration
}

// defaultTalosAPIRetryConfig returns the default transient-retry policy.
func defaultTalosAPIRetryConfig() talosAPIRetryConfig {
	return talosAPIRetryConfig{
		maxAttempts: defaultTalosAPIMaxAttempts,
		baseWait:    defaultTalosAPIRetryBaseWait,
		maxWait:     defaultTalosAPIRetryMaxWait,
	}
}

// isRetryableTransientTalosError reports whether err is a transient per-node
// Talos API failure worth retrying: a gRPC Unavailable or DeadlineExceeded
// status, or the apid TLS "authentication handshake failed" race that surfaces
// on the first RPC over a freshly established connection. Configuration and
// authorization errors are not transient and fall through to the caller
// unchanged.
func isRetryableTransientTalosError(err error) bool {
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
		strings.Contains(errMsg, "authentication handshake failed")
}

// retryTransientTalosAPICall runs operation, retrying it on transient per-node
// Talos API failures (see isRetryableTransientTalosError) with bounded
// exponential backoff per p.talosAPIRetry. target (a node IP or endpoint) and
// description supply log and error context only. Non-retryable errors are
// returned unchanged so callers' errors.Is checks keep working; when every
// attempt is exhausted the last error is wrapped with errRetriesExhausted.
//
// operation MUST be safe to run more than once. Callers performing a
// non-idempotent RPC (reboot, etcd leave, partition reset) should instead use
// dialTalosClientWithRetry, which absorbs the handshake race with an idempotent
// probe and then issues the real RPC exactly once.
func (p *Provisioner) retryTransientTalosAPICall(
	ctx context.Context,
	target string,
	description string,
	operation func() error,
) error {
	cfg := p.talosAPIRetry
	if cfg.maxAttempts < 1 {
		cfg.maxAttempts = 1
	}

	var lastErr error

	for attempt := 1; attempt <= cfg.maxAttempts; attempt++ {
		lastErr = operation()
		if lastErr == nil {
			return nil
		}

		if !isRetryableTransientTalosError(lastErr) {
			return lastErr
		}

		if attempt == cfg.maxAttempts {
			break
		}

		delay := netretry.ExponentialDelay(attempt, cfg.baseWait, cfg.maxWait)

		p.logf(
			"  %s attempt %d/%d failed on %s (retrying in %s): %v\n",
			description, attempt, cfg.maxAttempts, target, delay, lastErr,
		)

		sleepErr := sleepWithContext(ctx, delay)
		if sleepErr != nil {
			return fmt.Errorf("retry backoff interrupted: %w", sleepErr)
		}
	}

	return fmt.Errorf(
		"%s on %s: %w",
		description, target, errors.Join(errRetriesExhausted, lastErr),
	)
}

// withTalosClient creates an authenticated Talos client for nodeIP, runs fn, and
// closes the client — retrying the whole sequence (a fresh client per attempt)
// on transient apid failures. A fresh client is required because a stale client
// will not re-dial after a dropped connection.
//
// Use this for IDEMPOTENT operations (reads and declarative config applies),
// where re-running fn after a transient failure is harmless. For non-idempotent
// RPCs use dialTalosClientWithRetry instead.
func (p *Provisioner) withTalosClient(
	ctx context.Context,
	nodeIP string,
	description string,
	operation func(*talosclient.Client) error,
) error {
	return p.retryTransientTalosAPICall(ctx, nodeIP, description, func() error {
		client, err := p.createTalosClient(ctx, nodeIP)
		if err != nil {
			return err
		}

		defer client.Close() //nolint:errcheck

		return operation(client)
	})
}

// dialTalosClientWithRetry returns an authenticated Talos client for nodeIP
// whose connection has been warmed by a successful Version probe, retrying the
// create+probe on transient apid failures (a fresh client per attempt). gRPC
// dials lazily, so the flaky apid TLS handshake surfaces on the first RPC rather
// than on client creation; the idempotent Version probe absorbs that race here
// so the caller can issue a NON-IDEMPOTENT RPC (reboot, etcd leave, partition
// reset, or a multi-step upgrade workflow) exactly once over a connection
// already proven healthy. The caller owns the returned client and must Close it.
func (p *Provisioner) dialTalosClientWithRetry(
	ctx context.Context,
	nodeIP string,
	description string,
) (*talosclient.Client, error) {
	var client *talosclient.Client

	err := p.retryTransientTalosAPICall(ctx, nodeIP, description, func() error {
		candidate, createErr := p.createTalosClient(ctx, nodeIP)
		if createErr != nil {
			return createErr
		}

		_, probeErr := candidate.Version(ctx)
		if probeErr != nil {
			_ = candidate.Close()

			return fmt.Errorf("talos api warm-up probe failed: %w", probeErr)
		}

		client = candidate

		return nil
	})
	if err != nil {
		return nil, err
	}

	return client, nil
}
