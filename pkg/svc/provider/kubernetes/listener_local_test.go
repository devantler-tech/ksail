package kubernetes_test

import (
	"context"
	"testing"

	kubernetes "github.com/devantler-tech/ksail/v7/pkg/svc/provider/kubernetes"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewLocalListener_RandomPort(t *testing.T) {
	t.Parallel()

	port, closeFn, err := kubernetes.NewLocalListenerForTest(context.Background(), 0)
	require.NoError(t, err)

	defer func() { _ = closeFn() }()

	assert.Positive(t, port, "random port should be assigned")
}

func TestNewLocalListener_SpecificPort(t *testing.T) {
	t.Parallel()

	// Bind a random free port and keep it open for the duration of the test.
	port, closeFn, err := kubernetes.NewLocalListenerForTest(context.Background(), 0)
	require.NoError(t, err)

	defer func() { _ = closeFn() }()

	// Requesting that same (now-occupied) port must fail with "address already in use".
	// This deterministically proves the specific port is honored rather than silently
	// falling back to a random free port (which would have succeeded).
	_, closeFn2, err := kubernetes.NewLocalListenerForTest(context.Background(), port)
	if closeFn2 != nil {
		_ = closeFn2()
	}

	require.Error(t, err, "binding an already-bound specific port should fail")
}
