package uiserver_test

import (
	"context"
	"net"
	"strconv"
	"strings"
	"testing"

	"github.com/devantler-tech/ksail/v7/pkg/cli/uiserver"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestListenPicksFreePort verifies that port 0 binds to a kernel-chosen free port and that the
// returned URL reflects the actually bound port (the contract callers rely on to open a browser).
func TestListenPicksFreePort(t *testing.T) {
	t.Parallel()

	listener, url, err := uiserver.Listen(context.Background(), 0)
	require.NoError(t, err)

	t.Cleanup(func() { _ = listener.Close() })

	_, boundPort, splitErr := net.SplitHostPort(listener.Addr().String())
	require.NoError(t, splitErr)

	port, convErr := strconv.Atoi(boundPort)
	require.NoError(t, convErr)
	assert.Positive(t, port, "port 0 must resolve to a concrete kernel-assigned port")

	assert.Equal(t, "http://"+net.JoinHostPort(uiserver.Host, boundPort)+"/", url,
		"URL must advertise the actually bound port so callers can reach the server")
}

// TestListenBindsLoopbackOnly pins the security boundary documented on Host: the listener must bind
// to the loopback interface only, never a routable address, because the local API is unauthenticated.
func TestListenBindsLoopbackOnly(t *testing.T) {
	t.Parallel()

	listener, url, err := uiserver.Listen(context.Background(), 0)
	require.NoError(t, err)

	t.Cleanup(func() { _ = listener.Close() })

	tcpAddr, ok := listener.Addr().(*net.TCPAddr)
	require.True(t, ok, "expected a TCP listener address")
	assert.True(
		t,
		tcpAddr.IP.IsLoopback(),
		"listener must bind to loopback only, got %s",
		tcpAddr.IP,
	)
	assert.Equal(
		t,
		uiserver.Host,
		tcpAddr.IP.String(),
		"loopback bind must use the documented Host",
	)
	assert.True(
		t,
		strings.HasPrefix(url, "http://"+uiserver.Host+":"),
		"URL must point at the loopback host",
	)
}

// TestListenInvalidPort exercises the error path: an out-of-range port cannot be bound and the
// error is wrapped with the bind address for diagnosis.
func TestListenInvalidPort(t *testing.T) {
	t.Parallel()

	listener, url, err := uiserver.Listen(context.Background(), 99999)
	require.Error(t, err)
	assert.Nil(t, listener)
	assert.Empty(t, url)
	assert.Contains(t, err.Error(), "listen on", "error must be wrapped with the bind context")
}

// TestListenPortInUse verifies that attempting to bind an already-bound port surfaces an error
// rather than silently succeeding — a second server must not collide with a running one.
func TestListenPortInUse(t *testing.T) {
	t.Parallel()

	first, _, err := uiserver.Listen(context.Background(), 0)
	require.NoError(t, err)

	t.Cleanup(func() { _ = first.Close() })

	_, boundPort, splitErr := net.SplitHostPort(first.Addr().String())
	require.NoError(t, splitErr)

	port, convErr := strconv.Atoi(boundPort)
	require.NoError(t, convErr)

	second, url, err := uiserver.Listen(context.Background(), port)
	if second != nil {
		t.Cleanup(func() { _ = second.Close() })
	}

	require.Error(t, err, "binding an already-bound port must fail")
	assert.Nil(t, second)
	assert.Empty(t, url)
}

// TestNewServerWiring pins the constructor's wiring and the security-relevant defaults: the local
// server is read-write, advertises the creatable distributions, and serves the embedded SPA.
func TestNewServerWiring(t *testing.T) {
	t.Parallel()

	server := uiserver.NewServer()
	require.NotNil(t, server)
	assert.NotNil(t, server.Service, "server must be backed by a cluster service")
	assert.NotNil(t, server.StaticFS, "server must serve the embedded web UI assets")
	assert.False(t, server.ReadOnly, "the local UI server is read-write")
	assert.NotEmpty(t, server.Distributions, "server must advertise the creatable distributions")
}
