package talosprovisioner_test

import (
	"context"
	"io"
	"path/filepath"
	"testing"

	talosprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/talos"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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
		WithTalosAPIRetryConfig(testRetryMaxAttempts, testRetryBaseWait, testRetryMaxWait)
}

// dialTalosClientWithRetry warms the connection with an idempotent Version probe
// before the caller issues a non-idempotent RPC. A non-retryable client-creation
// error must surface immediately — without probing, returning a client, or being
// retried to exhaustion.
func TestDialTalosClientWithRetry_ClientCreationErrorShortCircuits(t *testing.T) {
	t.Parallel()

	provisioner := newClientErrProvisioner(t)

	client, err := provisioner.DialTalosClientWithRetryForTest(
		context.Background(), "1.2.3.4", "probe",
	)
	require.Error(t, err)
	assert.Nil(t, client)
	assert.NotErrorIs(t, err, talosprovisioner.ErrRetriesExhaustedForTest)
}
