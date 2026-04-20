package kwokprovisioner_test

import (
	"context"
	"errors"
	"testing"
	"time"

	kwokprovisioner "github.com/devantler-tech/ksail/v7/pkg/svc/provisioner/cluster/kwok"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	errRateLimit = errors.New(
		"error response from daemon: toomanyrequests: Quota exceeded for quota metric",
	)
	errQuotaExceeded = errors.New(
		"TOOMANYREQUESTS: Quota exceeded for quota metric 'Requests per project per region'",
	)
	errQuotaDetail = errors.New(
		"failed to pull image: Quota exceeded for service 'artifactregistry.googleapis.com'",
	)
	errIOTimeout    = errors.New("dial tcp 1.2.3.4:443: i/o timeout")
	errConnReset    = errors.New("read tcp 10.0.0.1:54321->1.2.3.4:443: connection reset by peer")
	errTLSTimeout   = errors.New("net/http: TLS handshake timeout")
	errNoSuchHost   = errors.New("dial tcp: lookup registry.k8s.io: no such host")
	errDNSTransient = errors.New(
		"dial tcp: lookup registry.k8s.io: temporary failure in name resolution",
	)
	errExitStatus1 = errors.New("exit status 1")
	errPermission  = errors.New("permission denied")
	errEmpty       = errors.New("")
)

// --- isTransientCreateError tests ---

func TestIsTransientCreateError(t *testing.T) {
	t.Parallel()

	for _, testCase := range []struct {
		name string
		err  error
		want bool
	}{
		{"rate_limit_toomanyrequests_is_transient", errRateLimit, true},
		{"rate_limit_TOOMANYREQUESTS_is_transient", errQuotaExceeded, true},
		{"quota_exceeded_is_transient", errQuotaDetail, true},
		{"io_timeout_is_transient", errIOTimeout, true},
		{"connection_reset_is_transient", errConnReset, true},
		{"tls_handshake_timeout_is_transient", errTLSTimeout, true},
		{"no_such_host_is_transient", errNoSuchHost, true},
		{"dns_temporary_failure_is_transient", errDNSTransient, true},
		{"exit_status_1_is_not_transient", errExitStatus1, false},
		{"permission_denied_is_not_transient", errPermission, false},
		{"empty_error_is_not_transient", errEmpty, false},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got := kwokprovisioner.IsTransientCreateErrorForTest(testCase.err)
			assert.Equal(t, testCase.want, got)
		})
	}
}

// --- createWithRetry tests ---

func TestCreateWithRetry_Success(t *testing.T) {
	t.Parallel()

	createCalls := 0
	create := func(_ context.Context) error {
		createCalls++

		return nil
	}

	cleanup := func(_ context.Context) {
		t.Error("cleanup should not be called on success")
	}

	err := kwokprovisioner.CreateWithRetryForTest(
		context.Background(),
		time.Millisecond,
		create, cleanup,
	)

	require.NoError(t, err)
	assert.Equal(t, 1, createCalls, "create should be called exactly once")
}

func TestCreateWithRetry_TransientErrorRetries(t *testing.T) {
	t.Parallel()

	createCalls := 0
	create := func(_ context.Context) error {
		createCalls++
		if createCalls < 2 {
			return errRateLimit
		}

		return nil
	}

	cleanupCalls := 0
	cleanup := func(_ context.Context) {
		cleanupCalls++
	}

	err := kwokprovisioner.CreateWithRetryForTest(
		context.Background(),
		time.Millisecond,
		create, cleanup,
	)

	require.NoError(t, err)
	assert.Equal(t, 2, createCalls, "create should be called twice")
	assert.Equal(t, 1, cleanupCalls, "cleanup should be called before the retry")
}

func TestCreateWithRetry_TransientErrorExhaustsAttempts(t *testing.T) {
	t.Parallel()

	createCalls := 0
	create := func(_ context.Context) error {
		createCalls++

		return errRateLimit
	}

	cleanupCalls := 0
	cleanup := func(_ context.Context) {
		cleanupCalls++
	}

	err := kwokprovisioner.CreateWithRetryForTest(
		context.Background(),
		time.Millisecond,
		create, cleanup,
	)

	require.Error(t, err)
	require.ErrorContains(t, err, "failed to create KWOK cluster after 3 attempts")
	require.ErrorIs(t, err, errRateLimit)
	assert.Equal(t, 3, createCalls, "create should be called maxAttempts times")
	assert.Equal(t, 2, cleanupCalls, "cleanup should be called before retries 2 and 3")
}

func TestCreateWithRetry_NonTransientErrorFailsImmediately(t *testing.T) {
	t.Parallel()

	createCalls := 0
	create := func(_ context.Context) error {
		createCalls++

		return errPermission
	}

	cleanup := func(_ context.Context) {
		t.Error("cleanup should not be called for non-transient errors")
	}

	err := kwokprovisioner.CreateWithRetryForTest(
		context.Background(),
		time.Millisecond,
		create, cleanup,
	)

	require.Error(t, err)
	require.ErrorIs(t, err, errPermission)
	assert.Equal(t, 1, createCalls, "create should be called exactly once")
}

func TestCreateWithRetry_ContextCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	createCalls := 0
	create := func(_ context.Context) error {
		createCalls++

		cancel()

		return errRateLimit
	}

	cleanupCalls := 0
	cleanup := func(_ context.Context) {
		cleanupCalls++
	}

	err := kwokprovisioner.CreateWithRetryForTest(
		ctx,
		time.Second,
		create, cleanup,
	)

	require.Error(t, err)
	require.ErrorContains(t, err, "context cancelled during create retry")
	assert.Equal(t, 1, createCalls, "create should be called once before context is cancelled")
	assert.Equal(t, 1, cleanupCalls, "cleanup should be called before the cancelled retry delay")
}
