package talosprovisioner

import (
	"context"

	talosclient "github.com/siderolabs/talos/pkg/machinery/client"
)

// IsTransientTalosAPIError exposes the unexported helper for tests in
// the talosprovisioner_test package.
//
//nolint:gochecknoglobals // export_test.go pattern requires global variables to expose internal functions
var IsTransientTalosAPIError = isTransientTalosAPIError

// ErrRetriesExhaustedForTest exposes errRetriesExhausted for tests in
// the talosprovisioner_test package.
var ErrRetriesExhaustedForTest = errRetriesExhausted

// RetryTransientTalosAPICallForTest exposes retryTransientTalosAPICall for unit testing.
func (p *Provisioner) RetryTransientTalosAPICallForTest(
	ctx context.Context,
	target, description string,
	operation func(ctx context.Context) error,
) error {
	return p.retryTransientTalosAPICall(ctx, target, description, operation)
}

// DialTalosClientWithRetryForTest exposes dialTalosClientWithRetry for unit testing.
func (p *Provisioner) DialTalosClientWithRetryForTest(
	ctx context.Context,
	nodeIP, description string,
) (*talosclient.Client, error) {
	return p.dialTalosClientWithRetry(ctx, nodeIP, description)
}
