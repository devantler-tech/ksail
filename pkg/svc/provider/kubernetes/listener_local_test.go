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

	// Grab a free port, release it, then bind it explicitly to verify the request is honored.
	port, closeFn, err := kubernetes.NewLocalListenerForTest(context.Background(), 0)
	require.NoError(t, err)
	require.NoError(t, closeFn())

	port2, closeFn2, err := kubernetes.NewLocalListenerForTest(context.Background(), port)
	require.NoError(t, err)

	defer func() { _ = closeFn2() }()

	assert.Equal(t, port, port2, "listener should bind the requested local port")
}
