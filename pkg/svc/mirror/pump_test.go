package mirror_test

import (
	"context"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/devantler-tech/ksail/v7/pkg/svc/mirror"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// pipeListener is an in-memory net.Listener the tests feed connections into,
// standing in for the steering agent's redirected-traffic listener.
type pipeListener struct {
	conns     chan net.Conn
	closeOnce sync.Once
	closed    chan struct{}
}

func newPipeListener() *pipeListener {
	return &pipeListener{
		conns:     make(chan net.Conn),
		closeOnce: sync.Once{},
		closed:    make(chan struct{}),
	}
}

func (l *pipeListener) Accept() (net.Conn, error) {
	select {
	case conn := <-l.conns:
		return conn, nil
	case <-l.closed:
		return nil, net.ErrClosed
	}
}

func (l *pipeListener) Close() error {
	l.closeOnce.Do(func() { close(l.closed) })

	return nil
}

func (l *pipeListener) Addr() net.Addr {
	return &net.UnixAddr{Name: "ksail-steer-test", Net: "unix"}
}

// deliver hands one redirected connection to the listener's Accept loop.
func (l *pipeListener) deliver(t *testing.T, conn net.Conn) {
	t.Helper()

	select {
	case l.conns <- conn:
	case <-l.closed:
		t.Fatal("delivering to a closed listener")
	case <-time.After(testTimeout):
		t.Fatal("timed out delivering a connection")
	}
}

// deadlined bounds every read/write on a pipe connection so a pump bug fails
// fast instead of hanging the suite.
func deadlined(t *testing.T, conn net.Conn) {
	t.Helper()

	require.NoError(t, conn.SetDeadline(time.Now().Add(testTimeout)))
}

// failingHalf is a ReadWriteCloser whose reads (and, when writeErr is set,
// writes) fail immediately, simulating a torn transport.
type failingHalf struct {
	readErr  error
	writeErr error
}

func (f *failingHalf) Read([]byte) (int, error) { return 0, f.readErr }

func (f *failingHalf) Write(p []byte) (int, error) {
	if f.writeErr != nil {
		return 0, f.writeErr
	}

	return len(p), nil
}

func (f *failingHalf) Close() error { return nil }

func TestPump_CopiesBothDirectionsAndTearsDownOnEOF(t *testing.T) {
	t.Parallel()

	outerLeft, innerLeft := net.Pipe()
	innerRight, outerRight := net.Pipe()

	deadlined(t, outerLeft)
	deadlined(t, outerRight)

	pumpDone := make(chan error, 1)

	go func() { pumpDone <- mirror.Pump(innerLeft, innerRight) }()

	go func() { _, _ = outerLeft.Write([]byte("ping")) }()

	buffer := make([]byte, 4)
	_, err := io.ReadFull(outerRight, buffer)
	require.NoError(t, err)
	assert.Equal(t, "ping", string(buffer))

	go func() { _, _ = outerRight.Write([]byte("pong")) }()

	_, err = io.ReadFull(outerLeft, buffer)
	require.NoError(t, err)
	assert.Equal(t, "pong", string(buffer))

	// Ending one side ends the pump and closes BOTH halves, so the other
	// side's peer sees EOF instead of a half-open connection.
	require.NoError(t, outerLeft.Close())

	select {
	case pumpErr := <-pumpDone:
		require.NoError(t, pumpErr)
	case <-time.After(testTimeout):
		t.Fatal("pump did not end after one side closed")
	}

	_, err = outerRight.Read(buffer)
	require.ErrorIs(t, err, io.EOF)
}

func TestPump_ReturnsTheErrorThatEndedTheFirstDirection(t *testing.T) {
	t.Parallel()

	_, inner := net.Pipe()
	failing := &failingHalf{readErr: errTransportTorn}

	err := mirror.Pump(failing, inner)
	require.ErrorIs(t, err, errTransportTorn)
}

func TestForwardRedirected_BridgesARedirectedConnectionRoundTrip(t *testing.T) {
	t.Parallel()

	client, server := newSessionPair(t)
	listener := newPipeListener()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	forwardDone := make(chan error, 1)

	go func() { forwardDone <- mirror.ForwardRedirected(ctx, listener, server) }()

	clusterSide, redirected := net.Pipe()
	deadlined(t, clusterSide)
	listener.deliver(t, redirected)

	stream, err := client.AcceptStream(acceptContext(t))
	require.NoError(t, err)

	go func() { _, _ = clusterSide.Write([]byte("request")) }()

	buffer := make([]byte, 7)
	_, err = io.ReadFull(stream, buffer)
	require.NoError(t, err)
	assert.Equal(t, "request", string(buffer))

	_, err = stream.Write([]byte("reply!!"))
	require.NoError(t, err)

	_, err = io.ReadFull(clusterSide, buffer)
	require.NoError(t, err)
	assert.Equal(t, "reply!!", string(buffer))

	cancel()

	select {
	case forwardErr := <-forwardDone:
		require.NoError(t, forwardErr)
	case <-time.After(testTimeout):
		t.Fatal("forward loop did not stop on context cancellation")
	}
}

func TestForwardRedirected_ReturnsNilWhenTheSessionIsClosed(t *testing.T) {
	t.Parallel()

	client, server := newSessionPair(t)
	listener := newPipeListener()

	forwardDone := make(chan error, 1)

	go func() {
		forwardDone <- mirror.ForwardRedirected(context.Background(), listener, server)
	}()

	// No connection ever arrives: ending the session must unblock the parked
	// Accept by itself.
	require.NoError(t, client.Close())
	require.NoError(t, server.Close())

	select {
	case forwardErr := <-forwardDone:
		require.NoError(t, forwardErr)
	case <-time.After(testTimeout):
		t.Fatal("forward loop did not stop after the session closed")
	}
}

func TestForwardRedirected_AFailedStreamOpenClosesTheConnection(t *testing.T) {
	t.Parallel()

	// A session whose transport reads block (the session stays alive) while
	// every write fails, so opening a stream fails without the session ending.
	blockedReader, _ := io.Pipe()

	t.Cleanup(func() { _ = blockedReader.Close() })

	session := mirror.NewTunnelSession(
		blockedReader, &failingHalf{writeErr: errTransportTorn}, mirror.TunnelRoleServer,
	)

	t.Cleanup(func() { _ = session.Close() })

	listener := newPipeListener()
	forwardDone := make(chan error, 1)

	go func() {
		forwardDone <- mirror.ForwardRedirected(context.Background(), listener, session)
	}()

	clusterSide, redirected := net.Pipe()
	deadlined(t, clusterSide)
	listener.deliver(t, redirected)

	select {
	case forwardErr := <-forwardDone:
		require.ErrorIs(t, forwardErr, errTransportTorn)
	case <-time.After(testTimeout):
		t.Fatal("forward loop did not stop after a failed stream open")
	}

	// The connection that could not become a stream is closed, not leaked.
	buffer := make([]byte, 1)
	_, err := clusterSide.Read(buffer)
	require.ErrorIs(t, err, io.EOF)
}

func TestServeIntercepted_DialsTheLocalProcessAndPumps(t *testing.T) {
	t.Parallel()

	client, server := newSessionPair(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	localApp, dialSide := net.Pipe()
	deadlined(t, localApp)

	serveDone := make(chan error, 1)

	go func() {
		serveDone <- mirror.ServeIntercepted(ctx, client,
			func(_ context.Context) (io.ReadWriteCloser, error) { return dialSide, nil })
	}()

	stream, err := server.OpenStream()
	require.NoError(t, err)

	_, err = stream.Write([]byte("hello"))
	require.NoError(t, err)

	buffer := make([]byte, 5)
	_, err = io.ReadFull(localApp, buffer)
	require.NoError(t, err)
	assert.Equal(t, "hello", string(buffer))

	go func() { _, _ = localApp.Write([]byte("world")) }()

	_, err = io.ReadFull(stream, buffer)
	require.NoError(t, err)
	assert.Equal(t, "world", string(buffer))

	cancel()

	select {
	case serveErr := <-serveDone:
		require.NoError(t, serveErr)
	case <-time.After(testTimeout):
		t.Fatal("serve loop did not stop on context cancellation")
	}
}

func TestServeIntercepted_AFailedDialClosesTheStream(t *testing.T) {
	t.Parallel()

	client, server := newSessionPair(t)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	dial := func(_ context.Context) (io.ReadWriteCloser, error) {
		return nil, errTransportTorn
	}

	go func() { _ = mirror.ServeIntercepted(ctx, client, dial) }()

	stream, err := server.OpenStream()
	require.NoError(t, err)

	// The refused stream is closed by the serving side: the agent's reads end
	// with EOF, which is what makes it close the redirected connection.
	buffer := make([]byte, 1)
	_, err = stream.Read(buffer)
	require.ErrorIs(t, err, io.EOF)
}

func TestServeIntercepted_ReturnsNilWhenTheSessionCloses(t *testing.T) {
	t.Parallel()

	client, server := newSessionPair(t)

	dial := func(_ context.Context) (io.ReadWriteCloser, error) {
		return nil, errTransportTorn
	}

	serveDone := make(chan error, 1)

	go func() {
		serveDone <- mirror.ServeIntercepted(context.Background(), client, dial)
	}()

	require.NoError(t, client.Close())
	require.NoError(t, server.Close())

	select {
	case serveErr := <-serveDone:
		require.NoError(t, serveErr)
	case <-time.After(testTimeout):
		t.Fatal("serve loop did not stop after the session closed")
	}
}

func TestServeIntercepted_SurfacesAProtocolErrorThatTearsTheTunnelDown(t *testing.T) {
	t.Parallel()

	// A steering channel that yields non-frame bytes (e.g. a stray banner the
	// agent image printed onto its stdout, the tunnel channel) corrupts the
	// demux and tears the session down with a codec error. ServeIntercepted must
	// surface it rather than returning nil — a corrupted tunnel is a diagnosable
	// failure, never a silent success.
	garbage := io.NopCloser(strings.NewReader("Fail to init k9s logs location\n"))
	session := mirror.NewTunnelSession(garbage, io.Discard, mirror.TunnelRoleClient)

	dial := func(_ context.Context) (io.ReadWriteCloser, error) {
		return nil, errTransportTorn
	}

	serveDone := make(chan error, 1)

	go func() {
		serveDone <- mirror.ServeIntercepted(context.Background(), session, dial)
	}()

	select {
	case serveErr := <-serveDone:
		require.Error(t, serveErr)
		require.ErrorIs(t, serveErr, mirror.ErrTunnelUnknownFrameType)
	case <-time.After(testTimeout):
		t.Fatal("serve loop did not return after the tunnel was corrupted")
	}
}

func TestInterceptPump_EndToEndEchoThroughTheTunnel(t *testing.T) {
	t.Parallel()

	client, server := newSessionPair(t)
	listener := newPipeListener()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)

	// Local "developer process": an echo server behind the dialer seam.
	dial := func(_ context.Context) (io.ReadWriteCloser, error) {
		localApp, dialSide := net.Pipe()

		go func() { _, _ = io.Copy(localApp, localApp) }()

		return dialSide, nil
	}

	go func() { _ = mirror.ForwardRedirected(ctx, listener, server) }()
	go func() { _ = mirror.ServeIntercepted(ctx, client, dial) }()

	clusterSide, redirected := net.Pipe()
	deadlined(t, clusterSide)
	listener.deliver(t, redirected)

	go func() { _, _ = clusterSide.Write([]byte("echo-me")) }()

	buffer := make([]byte, 7)
	_, err := io.ReadFull(clusterSide, buffer)
	require.NoError(t, err)
	assert.Equal(t, "echo-me", string(buffer))
}
